// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"crypto/aes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/spf13/cast"
	"github.com/stolostron/kubernetes-dependency-watches/client"
	yaml "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

const (
	defaultStartDelim = "{{"
	defaultStopDelim  = "}}"
	IVSize            = 16 // Size in bytes
	protectedPrefix   = "$ocm_encrypted:"
	yamlIndentation   = 2
)

var (
	ErrAESKeyNotSet                      = errors.New("AESKey must be set to use this encryption mode")
	ErrInvalidAESKey                     = errors.New("the AES key is invalid")
	ErrInvalidB64OfEncrypted             = errors.New("the encrypted string is invalid base64")
	ErrIVNotSet                          = errors.New("initialization vector must be set to use this encryption mode")
	ErrInvalidIV                         = errors.New("initialization vector must be 128 bits")
	ErrInvalidPKCS7Padding               = errors.New("invalid PCKS7 padding")
	ErrMissingAPIResource                = errors.New("one or more API resources are not installed on the API server")
	ErrMissingAPIResourceInvalidTemplate = errors.New(
		"one or more API resources are not installed on the API server which could have led to the templating error",
	)
	ErrProtectNotEnabled  = errors.New("the protect template function is not enabled in this mode")
	ErrNewLinesNotAllowed = errors.New("new lines are not allowed in the string passed to the toLiteral function")
)

// Config is a struct containing configuration for the API. Some are required.
//
// - AdditionalIndentation sets the number of additional spaces to be added to the input number
// to the indent method. This is useful in situations when the indentation should be relative
// to a logical starting point in a YAML file.
//
// - DisabledFunctions is a slice of default template function names that should be disabled.
// - KubeAPIResourceList sets the cache for the Kubernetes API resources. If this is
// set, template processing will not try to rediscover the Kubernetes API resources
// needed for dynamic client/ GVK lookups.
//
// - EncryptionConfig is the configuration for template encryption/decryption functionality.
//
// - InitializationVector is the initialization vector (IV) used in the AES-CBC encryption/decryption. Note that it must
// be equal to the AES block size which is always 128 bits (16 bytes). This value must be random but does not need to be
// private. Its purpose is to make the same plaintext value, when encrypted with the same AES key, appear unique. When
// performing decryption, the IV must be the same as it was for the encryption of the data. Note that all values
// encrypted in the template will use this same IV, which means that duplicate plaintext values that are encrypted will
// yield the same encrypted value in the template.
//
// - LookupNamespace is the namespace to restrict "lookup" template functions (e.g. fromConfigMap)
// to. If this is not set (i.e. an empty string), then all namespaces can be used.
//
// - StartDelim customizes the start delimiter used to distinguish a template action. This defaults
// to "{{". If StopDelim is set, this must also be set.
//
// - StopDelim customizes the stop delimiter used to distinguish a template action. This defaults
// to "}}". If StartDelim is set, this must also be set.
type Config struct {
	AdditionalIndentation uint
	DisabledFunctions     []string
	EncryptionConfig
	KubeAPIResourceList []*metav1.APIResourceList
	LookupNamespace     string
	StartDelim          string
	StopDelim           string
}

// EncryptionConfig is a struct containing configuration for template encryption/decryption functionality.
//
// - AESKey is an AES key (e.g. AES-256) to use for the "protect" template function and decrypting
// such values.
//
// - AESKeyFallback is an AES key to try if the decryption fails using AESKey.
//
// - DecryptionConcurrency is the concurrency (i.e. number of Goroutines) limit when decrypting encrypted strings. Not
// setting this value is the equivalent of setting this to 1, which means no concurrency.
//
// - DecryptionEnabled enables automatic decrypting of encrypted strings. AESKey and InitializationVector must also be
// set if this is enabled.
//
// - EncryptionEnabled enables the "protect" template function and "fromSecret" returns encrypted content. AESKey and
// InitializationVector must also be set if this is enabled.
//
// - InitializationVector is the initialization vector (IV) used in the AES-CBC encryption/decryption. Note that it must
// be equal to the AES block size which is always 128 bits (16 bytes). This value must be random but does not need to be
// private. Its purpose is to make the same plaintext value, when encrypted with the same AES key, appear unique. When
// performing decryption, the IV must be the same as it was for the encryption of the data. Note that all values
// encrypted in the template will use this same IV, which means that duplicate plaintext values that are encrypted will
// yield the same encrypted value in the template.
type EncryptionConfig struct {
	AESKey                []byte
	AESKeyFallback        []byte
	DecryptionConcurrency uint8
	DecryptionEnabled     bool
	EncryptionEnabled     bool
	InitializationVector  []byte
}

// TemplateResolver is the API for processing templates. It's better to use the NewResolver function
// instead of instantiating this directly so that configuration defaults and validation are applied.
type TemplateResolver struct {
	kubeClient *kubernetes.Interface
	kubeConfig *rest.Config
	config     Config
	// Denotes if the lookup template function encountered an API resource that is not installed on
	// the Kubernetes API server.
	missingAPIResource bool
	referencedObjects  []client.ObjectIdentifier
}

type TemplateResult struct {
	ResolvedJSON      []byte
	ReferencedObjects []client.ObjectIdentifier
}

// NewResolver creates a new TemplateResolver instance, which is the API for processing templates.
//
// - kubeClient is the Kubernetes client to be used for the template lookup functions.
//
// - config is the Config instance for configuration for template processing.
func NewResolver(kubeClient *kubernetes.Interface, kubeConfig *rest.Config, config Config) (*TemplateResolver, error) {
	if kubeClient == nil {
		return nil, fmt.Errorf("kubeClient must be a non-nil value")
	}

	err := validateEncryptionConfig(config.EncryptionConfig)
	if err != nil {
		return nil, fmt.Errorf("error validating EncryptionConfig: %w", err)
	}

	if (config.StartDelim != "" && config.StopDelim == "") || (config.StartDelim == "" && config.StopDelim != "") {
		return nil, fmt.Errorf("the configurations StartDelim and StopDelim cannot be set independently")
	}

	// It's only required to check config.StartDelim since it's invalid to set these independently
	if config.StartDelim == "" {
		config.StartDelim = defaultStartDelim
		config.StopDelim = defaultStopDelim
	}

	klog.V(2).Infof("Using the delimiters of %s and %s", config.StartDelim, config.StopDelim)

	return &TemplateResolver{
		kubeClient: kubeClient,
		kubeConfig: kubeConfig,
		config:     config,
	}, nil
}

// HasTemplate performs a simple check for the template start delimiter or the "$ocm_encrypted" prefix
// (checkForEncrypted must be set to true) to indicate if the input byte slice has a template. If the startDelim
// argument is an empty string, the default start delimiter of "{{" will be used.
func HasTemplate(template []byte, startDelim string, checkForEncrypted bool) bool {
	if startDelim == "" {
		startDelim = defaultStartDelim
	}

	templateStr := string(template)
	klog.V(2).Infof("HasTemplate template str:  %v", templateStr)
	klog.V(2).Infof("Checking for the start delimiter:  %s", startDelim)

	hasTemplate := false
	if strings.Contains(templateStr, startDelim) {
		hasTemplate = true
	} else if checkForEncrypted && strings.Contains(templateStr, protectedPrefix) {
		hasTemplate = true
	}

	klog.V(2).Infof("hasTemplate: %v", hasTemplate)

	return hasTemplate
}

// UsesEncryption searches for templates that would generate encrypted values and returns a boolean
// whether one was found.
func UsesEncryption(template []byte, startDelim string, stopDelim string) bool {
	if startDelim == "" {
		startDelim = defaultStartDelim
	}

	if stopDelim == "" {
		stopDelim = defaultStopDelim
	}

	templateStr := string(template)
	klog.V(2).Infof("usesEncryption template str:  %v", templateStr)
	klog.V(2).Infof("Checking for encryption functions")

	// Check for encryption template functions:
	// {{ fromSecret ... }}
	// {{ ... | protect }}
	d1 := regexp.QuoteMeta(startDelim)
	d2 := regexp.QuoteMeta(stopDelim)
	re := regexp.MustCompile(d1 + `(\s*fromSecret\s+.*|.*\|\s*protect\s*)` + d2)
	usesEncryption := re.MatchString(templateStr)

	klog.V(2).Infof("usesEncryption: %v", usesEncryption)

	return usesEncryption
}

// getValidContext takes an input context struct with string fields and
// validates it. If is is valid, the context will be returned as is. If the
// input context is nil, an empty struct will be returned. If it's not valid, an
// error will be returned.
func getValidContext(context interface{}) (ctx interface{}, _ error) {
	var ctxType reflect.Type

	if context == nil {
		ctx = struct{}{}

		return ctx, nil
	}

	ctxType = reflect.TypeOf(context)
	if ctxType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("the input context must be a struct with string fields, got %s", ctxType)
	}

	for i := 0; i < ctxType.NumField(); i++ {
		f := ctxType.Field(i)
		if f.Type.Kind() != reflect.String {
			return nil, errors.New("the input context must be a struct with string fields")
		}
	}

	return context, nil
}

// SetKubeAPIResourceList overrides the KubeAPIResourceList value on the TemplateResolver
// configuration.
func (t *TemplateResolver) SetKubeAPIResourceList(resourceList []*metav1.APIResourceList) {
	t.config.KubeAPIResourceList = resourceList
}

// SetEncryptionConfig accepts an EncryptionConfig struct and validates it to ensure that if
// encryption and/or decryption are enabled that the AES Key and Initialization Vector are valid. If
// validation passes, SetEncryptionConfig updates the EncryptionConfig in the TemplateResolver
// configuration. Otherwise, an error is returned and the configuration is unchanged.
func (t *TemplateResolver) SetEncryptionConfig(encryptionConfig EncryptionConfig) error {
	klog.V(2).Info("Setting EncryptionConfig for templates")

	err := validateEncryptionConfig(encryptionConfig)
	if err != nil {
		return err
	}

	t.config.EncryptionConfig = encryptionConfig

	return nil
}

// validateEncryptionConfig validates an EncryptionConfig struct to to ensure that if encryption
// and/or decryption are enabled that the AES Key and Initialization Vector are valid.
func validateEncryptionConfig(encryptionConfig EncryptionConfig) error {
	if encryptionConfig.EncryptionEnabled || encryptionConfig.DecryptionEnabled {
		// Ensure AES Key is set
		if encryptionConfig.AESKey == nil {
			return ErrAESKeyNotSet
		}
		// Validate AES Key
		_, err := aes.NewCipher(encryptionConfig.AESKey)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAESKey, err)
		}

		// Validate the fallback AES Key
		if encryptionConfig.AESKeyFallback != nil {
			_, err = aes.NewCipher(encryptionConfig.AESKeyFallback)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidAESKey, err)
			}
		}

		// Ensure Initialization Vector is set
		if encryptionConfig.InitializationVector == nil {
			return ErrIVNotSet
		}
		// AES uses a 128 bit (16 byte) block size no matter the key size. The initialization vector
		// must be the same length as the block size.
		if len(encryptionConfig.InitializationVector) != IVSize {
			return ErrInvalidIV
		}

		if encryptionConfig.EncryptionEnabled {
			klog.V(2).Info("Template encryption is enabled")
		}

		if encryptionConfig.DecryptionEnabled {
			klog.V(2).Info("Template decryption is enabled")
		}
	} else {
		klog.V(2).Info("Template encryption and decryption is disabled")
	}

	return nil
}

// ResolveTemplate accepts a map marshaled as JSON. It also accepts a struct
// with string fields that will be made available when the template is processed.
// For example, if the argument is `struct{ClusterName string}{"cluster1"}`,
// the value `cluster1` would be available with `{{ .ClusterName }}`. This can
// also be `nil` if no fields should be made available.
//
// ResolveTemplate will process any template strings in the map and return the processed map. The
// ErrMissingAPIResource is returned when one or more "lookup" calls referenced an API resource
// which isn't installed on the Kubernetes API server. In this case, the resolved template is still
// returned. ErrMissingAPIResourceInvalidTemplate can also be returned in this case but it also means the template
// failed to resolve, so the resolved template will not be returned.
func (t *TemplateResolver) ResolveTemplate(tmplJSON []byte, context interface{}) (TemplateResult, error) {
	klog.V(2).Infof("ResolveTemplate for: %v", string(tmplJSON))

	// Always reset this value on each ResolveTemplate call.
	t.missingAPIResource = false
	t.referencedObjects = []client.ObjectIdentifier{}

	var resolvedResult TemplateResult

	ctx, err := getValidContext(context)
	if err != nil {
		return resolvedResult, err
	}

	// Build Map of supported template functions
	funcMap := template.FuncMap{
		"fromSecret":       t.fromSecret,
		"fromConfigMap":    t.fromConfigMap,
		"fromClusterClaim": t.fromClusterClaim,
		"lookup":           t.lookup,
		"base64enc":        base64encode,
		"base64dec":        base64decode,
		"autoindent":       autoindent,
		"indent":           t.indent,
		"atoi":             atoi,
		"toInt":            toInt,
		"toBool":           toBool,
		"toLiteral":        toLiteral,
	}

	// Add all the functions from sprig we will support
	for _, fname := range exportedSprigFunctions {
		funcMap[fname] = getSprigFunc(fname)
	}

	if t.config.EncryptionEnabled {
		funcMap["fromSecret"] = t.fromSecretProtected
		funcMap["protect"] = t.protect
	} else {
		// In other encryption modes, return a readable error if the protect template function is accidentally used.
		funcMap["protect"] = func(s string) (string, error) { return "", ErrProtectNotEnabled }
	}

	for _, funcName := range t.config.DisabledFunctions {
		delete(funcMap, funcName)
	}

	// create template processor and Initialize function map
	tmpl := template.New("tmpl").Delims(t.config.StartDelim, t.config.StopDelim).Funcs(funcMap)

	// convert the JSON to YAML
	templateYAMLBytes, err := jsonToYAML(tmplJSON)
	if err != nil {
		return resolvedResult, fmt.Errorf("failed to convert the policy template to YAML: %w", err)
	}

	templateStr := string(templateYAMLBytes)
	klog.V(2).Infof("Initial template str to resolve : %v ", templateStr)

	if t.config.DecryptionEnabled {
		templateStr, err = t.processEncryptedStrs(templateStr)
		if err != nil {
			return resolvedResult, err
		}
	}

	// process for int or bool
	if strings.Contains(templateStr, "toInt") ||
		strings.Contains(templateStr, "toBool") ||
		strings.Contains(templateStr, "toLiteral") {
		templateStr = t.processForDataTypes(templateStr)
	}

	// convert `autoindent` placeholders to `indent N`
	if strings.Contains(templateStr, "autoindent") {
		templateStr = t.processForAutoIndent(templateStr)
	}

	tmpl, err = tmpl.Parse(templateStr)
	if err != nil {
		tmplJSONStr := string(tmplJSON)
		klog.Errorf(
			"error parsing template JSON string %v,\n template str %v,\n error: %v", tmplJSONStr, templateStr, err,
		)

		return resolvedResult, fmt.Errorf("failed to parse the template JSON string %v: %w", tmplJSONStr, err)
	}

	var buf strings.Builder

	err = tmpl.Execute(&buf, ctx)

	resolvedResult.ReferencedObjects = t.referencedObjects

	if err != nil {
		tmplJSONStr := string(tmplJSON)
		klog.Errorf("error resolving the template %v,\n template str %v,\n error: %v", tmplJSONStr, templateStr, err)

		if t.missingAPIResource {
			return resolvedResult, fmt.Errorf("%w: %v: %v", ErrMissingAPIResourceInvalidTemplate, err, tmplJSONStr)
		}

		return resolvedResult, fmt.Errorf("failed to resolve the template %v: %w", tmplJSONStr, err)
	}

	resolvedTemplateStr := buf.String()
	klog.V(3).Infof("resolved template str: %v ", resolvedTemplateStr)
	// unmarshall before returning

	resolvedTemplateBytes, err := yamlToJSON([]byte(resolvedTemplateStr))
	if err != nil {
		return resolvedResult, fmt.Errorf("failed to convert the resolved template back to YAML: %w", err)
	}

	if t.missingAPIResource {
		return resolvedResult, ErrMissingAPIResource
	}

	resolvedResult.ResolvedJSON = resolvedTemplateBytes

	return resolvedResult, nil
}

// nolint: wsl
func (t *TemplateResolver) processForDataTypes(str string) string {
	// The idea is to remove the quotes enclosing the template if it has toBool, toInt, or toLiteral.
	// Quotes around the resolved template forces the value to be a string so removal of these quotes allows YAML to
	// process the datatype correctly.

	// the below pattern searches for optional block scalars | or >.. followed by the quoted template ,
	// and replaces it with just the template txt thats inside the outer quotes
	// ex-1 key : '{{ "6" | toInt }}'  .. is replaced with  key : {{ "6" | toInt }}
	// ex-2 key : |
	//						'{{ "true" | toBool }}' .. is replaced with key : {{ "true" | toBool }}

	// NOTES : on testing it was found that
	// outer quotes around key-values are always single quotes
	// even if the user input is with  double quotes , the yaml processed and saved with single quotes

	d1 := regexp.QuoteMeta(t.config.StartDelim)
	d2 := regexp.QuoteMeta(t.config.StopDelim)
	re := regexp.MustCompile(
		`:\s+(?:[\|>]-?\s+)?(?:'?\s*)(` + d1 + `.*\|\s*(?:toInt|toBool|toLiteral).*` + d2 + `)(?:\s*'?)`,
	)
	klog.V(2).Infof("\n Pattern: %v\n", re.String())

	submatchall := re.FindAllStringSubmatch(str, -1)
	klog.V(2).Infof("\n All Submatches:\n%v", submatchall)

	processeddata := re.ReplaceAllString(str, ": $1")
	klog.V(2).Infof("\n processed data :\n%v", processeddata)

	return processeddata
}

// processForAutoIndent converts any `autoindent` placeholders into `indent N` in the string.
// The processed input string is returned.
func (t *TemplateResolver) processForAutoIndent(str string) string {
	d1 := regexp.QuoteMeta(t.config.StartDelim)
	d2 := regexp.QuoteMeta(t.config.StopDelim)
	// Detect any templates that contain `autoindent` and capture the spaces before it.
	// Later on, the amount of spaces will dictate the conversion of `autoindent` to `indent`.
	// This is not a very strict regex as occasionally, a user will make a mistake such as
	// `config: '{{ "hello\nworld" | autoindent }}'`. In that event, `autoindent` will change to
	// `indent 1`, but `indent` properly handles this.
	re := regexp.MustCompile(`( *)(?:'|")?(` + d1 + `.*\| *autoindent *` + d2 + `)`)
	klog.V(2).Infof("\n Pattern: %v\n", re.String())

	submatches := re.FindAllStringSubmatch(str, -1)
	processed := str

	klog.V(2).Infof("\n All Submatches:\n%v", submatches)

	for _, submatch := range submatches {
		numSpaces := len(submatch[1]) - int(t.config.AdditionalIndentation)
		matchStr := submatch[2]
		newMatchStr := strings.Replace(matchStr, "autoindent", fmt.Sprintf("indent %d", numSpaces), 1)
		processed = strings.Replace(processed, matchStr, newMatchStr, 1)
	}

	klog.V(2).Infof("\n processed data :\n%v", processed)

	return processed
}

// jsonToYAML converts JSON to YAML using yaml.v3. This is important since
// line wrapping is disabled in v3.
func jsonToYAML(j []byte) ([]byte, error) {
	// Convert the JSON to an object
	var jsonObj interface{}

	err := yaml.Unmarshal(j, &jsonObj)
	if err != nil {
		return nil, err // nolint:wrapcheck
	}

	// Marshal this object into YAML
	var b bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&b)
	yamlEncoder.SetIndent(yamlIndentation)

	err = yamlEncoder.Encode(&jsonObj)
	if err != nil {
		return nil, err // nolint:wrapcheck
	}

	return b.Bytes(), nil
}

// yamlToJSON converts YAML to JSON.
func yamlToJSON(y []byte) ([]byte, error) {
	// Convert the YAML to an object.
	var yamlObj interface{}

	err := yaml.Unmarshal(y, &yamlObj)
	if err != nil {
		return nil, err // nolint:wrapcheck
	}

	// Convert this object to JSON
	return json.Marshal(yamlObj) // nolint:wrapcheck
}

func (t *TemplateResolver) indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces+int(t.config.AdditionalIndentation))
	npad := "\n" + pad + strings.Replace(v, "\n", "\n"+pad, -1)

	return strings.TrimSpace(npad)
}

// This is so that the user gets a nicer error in the event some valid scenario slips through the
// regex.
func autoindent(v string) (string, error) {
	return "", errors.New("an unexpected error occurred where autoindent could not be processed")
}

func toInt(v interface{}) int {
	return cast.ToInt(v)
}

func atoi(a string) int {
	i, _ := strconv.Atoi(a)

	return i
}

func toBool(a string) bool {
	b, _ := strconv.ParseBool(a)

	return b
}

// toLiteral just returns the input string as it is, however, this template function will be used to detect when
// to remove quotes around the template string after the template is processed.
func toLiteral(a string) (string, error) {
	if strings.Contains(a, "\n") {
		return "", ErrNewLinesNotAllowed
	}

	return a, nil
}

func (t *TemplateResolver) addToReferencedObjects(apiversion string, kind string, namespace string, name string) {
	gvk := schema.FromAPIVersionAndKind(apiversion, kind)

	objID := client.ObjectIdentifier{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
	}
	t.referencedObjects = append(t.referencedObjects, objID)
}
