// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cast"
	"github.com/stolostron/kubernetes-dependency-watches/client"
	yaml "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	defaultStartDelim = "{{"
	defaultStopDelim  = "}}"
	IVSize            = 16 // Size in bytes
	protectedPrefix   = "$ocm_encrypted:"
	yamlIndentation   = 2
)

var (
	ErrAESKeyNotSet          = errors.New("AESKey must be set to use this encryption mode")
	ErrInvalidAESKey         = errors.New("the AES key is invalid")
	ErrInvalidB64OfEncrypted = errors.New("the encrypted string is invalid base64")
	ErrIVNotSet              = errors.New("initialization vector must be set to use this encryption mode")
	ErrInvalidIV             = errors.New("initialization vector must be 128 bits")
	ErrInvalidPKCS7Padding   = errors.New("invalid PCKS7 padding")
	ErrMissingAPIResource    = errors.New("one or more API resources are not installed on the API server")
	ErrProtectNotEnabled     = errors.New("the protect template function is not enabled in this mode")
	ErrNewLinesNotAllowed    = errors.New("new lines are not allowed in the string passed to the toLiteral function")
	ErrInvalidContextType    = errors.New(
		"the input context must be a struct, with either string fields or map[string]string fields",
	)
	ErrMissingNamespace = errors.New(
		"the lookup of a single namespaced resource must have a namespace specified",
	)
	ErrRestrictedNamespace      = errors.New("the namespace argument is restricted")
	ErrInvalidInput             = errors.New("the input is invalid")
	ErrCacheDisabled            = client.ErrCacheDisabled
	ErrNoCacheEntry             = client.ErrNoCacheEntry
	ErrContextTransformerFailed = errors.New("the context transformer failed")
)

// Config is a struct containing configuration for the API.
//
// - AdditionalIndentation sets the number of additional spaces to be added to the input number
// to the indent method. This is useful in situations when the indentation should be relative
// to a logical starting point in a YAML file.
//
// - DisabledFunctions is a slice of default template function names that should be disabled.
//
// - StartDelim customizes the start delimiter used to distinguish a template action. This defaults
// to "{{". If StopDelim is set, this must also be set.
//
// - StopDelim customizes the stop delimiter used to distinguish a template action. This defaults
// to "}}". If StartDelim is set, this must also be set.
//
// - MissingAPIResourceCacheTTL can be set if you want to temporarily cache an API resource is missing to avoid
// duplicate API queries when a CRD is missing. By default, this will not be cached. Note that this only affects
// when caching is enabled.
type Config struct {
	AdditionalIndentation uint
	DisabledFunctions     []string
	StartDelim            string
	StopDelim             string

	MissingAPIResourceCacheTTL time.Duration
}

// ResolveOptions is a struct containing configuration for calling ResolveTemplate.
//
// - ContextTransformers is a list of functions that can modify the input context to ResolveTemplate using the caching
// query API. This is useful if you want to add information about a Kubernetes object in the context and be notified
// when the object changes.
//
// - ClusterScopedAllowList is a list of cluster-scoped object identifiers (group, kind, name) which
// are allowed to be used in "lookup" calls even when LookupNamespace is set. A wildcard value `*`
// may be used in any or all of the fields. The default behavior when LookupNamespace is set is to
// deny all cluster-scoped lookups.
//
// - EncryptionConfig is the configuration for template encryption/decryption functionality.
//
// - DisableAutoCacheCleanUp will not clean up stale API watches and cache entries after ResolveTemplate is called.
// The caller must call the CacheCleanUp function returned from ResolveTemplate when done. This is useful if you are
// splitting up calls to ResolveTemplate for a single template owner object.
//
// - InputIsYAML can be set to true to indicate that the input to the template is already in YAML format and thus does
// not need to be converted from JSON to YAML before template processing occurs. This should be set to true when
// passing raw YAML directly to the template resolver.
//
// - LookupNamespace is the namespace to restrict "lookup" template functions (e.g. fromConfigMap)
// to. If this is not set (i.e. an empty string), then all namespaces can be used.
//
// - Watcher is the Kubernetes object that includes the templates. This is only used when caching is enabled.
type ResolveOptions struct {
	ContextTransformers []func(
		queryAPI CachingQueryAPI, context interface{},
	) (transformedContext interface{}, err error)
	ClusterScopedAllowList []ClusterScopedObjectIdentifier
	EncryptionConfig
	DisableAutoCacheCleanUp bool
	InputIsYAML             bool
	LookupNamespace         string
	Watcher                 *client.ObjectIdentifier
}

type ClusterScopedObjectIdentifier struct {
	Group string
	Kind  string
	Name  string
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
	config Config
	// Used when caching is disabled.
	dynamicClient *dynamic.DynamicClient
	kubeConfig    *rest.Config
	// Used when instantiated with NewResolverWithCaching. This will create watches and the cache will get
	// automatically updated.
	dynamicWatcher client.DynamicWatcher
	// If caching is disabled, this will act as a temporary cache for objects during the execution of the
	// ResolveTemplate call.
	tempCallCache client.ObjectCache
	// When a pre-existing DynamicWatcher is used, let the caller fully manage the QueryBatch.
	skipBatchManagement bool
}

type CacheCleanUpFunc func() error

type TemplateResult struct {
	ResolvedJSON []byte
	CacheCleanUp CacheCleanUpFunc
	// HasSensitiveData is true if a template references a secret or decrypts an encrypted value.
	HasSensitiveData bool
}

// NewResolver creates a new TemplateResolver instance, which is the API for processing templates.
//
// - kubeConfig is the rest.Config instance used to create Kubernetes clients for template processing.
//
// - config is the Config instance for configuring optional values for template processing.
func NewResolver(kubeConfig *rest.Config, config Config) (*TemplateResolver, error) {
	if (config.StartDelim != "" && config.StopDelim == "") || (config.StartDelim == "" && config.StopDelim != "") {
		return nil, fmt.Errorf("the configurations StartDelim and StopDelim cannot be set independently")
	}

	// It's only required to check config.StartDelim since it's invalid to set these independently
	if config.StartDelim == "" {
		config.StartDelim = defaultStartDelim
		config.StopDelim = defaultStopDelim
	}

	klog.V(2).Infof("Using the delimiters of %s and %s", config.StartDelim, config.StopDelim)

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	tempCallCache := client.NewObjectCache(
		// Set the missing API resource cache TTL in this mode because the cache just lives for the ResolveTemplate
		// execution and duplicate queries when a CRD is missing is not necessary.
		discoveryClient, client.ObjectCacheOptions{
			MissingAPIResourceCacheTTL: time.Minute,
			UnsafeDisableDeepCopy:      false,
		},
	)

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	return &TemplateResolver{
		config: config, dynamicClient: dynamicClient, kubeConfig: kubeConfig, tempCallCache: tempCallCache,
	}, nil
}

// NewResolverWithCaching creates a new caching TemplateResolver instance, which is the API for processing templates.
//
// The caching works by adding watches to the objects and list queries used in the templates. A controller-runtime
// Channel is also returned to trigger reconciles on the watched object provided in ResolveTemplate when a watched
// object is added, updated, or removed.
//
//   - ctx should be a cancelable context that should be canceled when you want the background goroutines involving
//     caching to be stopped.
//
//   - kubeConfig is the rest.Config instance used to create Kubernetes clients for template processing.
//
//   - config is the Config instance for configuring optional values for template processing.
func NewResolverWithCaching(
	ctx context.Context, kubeConfig *rest.Config, config Config,
) (
	*TemplateResolver, *source.Channel, error,
) {
	resolver, err := NewResolver(kubeConfig, config)
	if err != nil {
		return nil, nil, err
	}

	reconciler, channel := client.NewControllerRuntimeSource()
	dynamicWatcher, err := client.New(
		kubeConfig,
		reconciler,
		&client.Options{
			DisableInitialReconcile: true,
			EnableCache:             true,
			ObjectCacheOptions: client.ObjectCacheOptions{
				MissingAPIResourceCacheTTL: config.MissingAPIResourceCacheTTL,
				UnsafeDisableDeepCopy:      false,
			},
		},
	)

	go func() {
		err = dynamicWatcher.Start(ctx)
	}()

	<-dynamicWatcher.Started()

	resolver.dynamicWatcher = dynamicWatcher
	resolver.dynamicClient = nil
	resolver.tempCallCache = nil

	return resolver, channel, err
}

// NewResolverWithDynamicWatcher creates a new caching TemplateResolver instance, using the provided dependency-watcher.
// The caller is responsible for managing the given DynamicWatcher, including starting and stopping it. The caller must
// start a query batch on the DynamicWatcher for the "watcher" object before calling ResolveTemplate.
//
// - dynWatcher is an already running DynamicWatcher from kubernetes-dependency-watches.
//
// - config is the Config instance for configuring optional values for template processing.
func NewResolverWithDynamicWatcher(dynWatcher client.DynamicWatcher, config Config) (*TemplateResolver, error) {
	if (config.StartDelim != "" && config.StopDelim == "") || (config.StartDelim == "" && config.StopDelim != "") {
		return nil, fmt.Errorf("the configurations StartDelim and StopDelim cannot be set independently")
	}

	// It's only required to check config.StartDelim since it's invalid to set these independently
	if config.StartDelim == "" {
		config.StartDelim = defaultStartDelim
		config.StopDelim = defaultStopDelim
	}

	return &TemplateResolver{
		config:              config,
		dynamicClient:       nil,
		kubeConfig:          nil,
		dynamicWatcher:      dynWatcher,
		tempCallCache:       nil,
		skipBatchManagement: true,
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
	// {{ copySecretData ... }}
	// {{ ... | protect }}
	d1 := regexp.QuoteMeta(startDelim)
	d2 := regexp.QuoteMeta(stopDelim)
	re := regexp.MustCompile(d1 + `(\s*fromSecret\s+.*|\s*copySecretData\s+.*|.*\|\s*protect\s*)` + d2)
	usesEncryption := re.MatchString(templateStr)

	klog.V(2).Infof("usesEncryption: %v", usesEncryption)

	return usesEncryption
}

// getValidContext takes an input context struct with string fields and
// validates it. If it is valid, the context will be returned as is. If the
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
		return nil, fmt.Errorf("%w, got %s", ErrInvalidContextType, ctxType)
	}

	for i := 0; i < ctxType.NumField(); i++ {
		f := ctxType.Field(i)

		switch f.Type.Kind() {
		case reflect.String:
			// good
		case reflect.Map:
			// check if it's map[string]string
			if f.Type.Elem().Kind() != reflect.String || f.Type.Key().Kind() != reflect.String {
				return nil, ErrInvalidContextType
			}
		default:
			return nil, ErrInvalidContextType
		}
	}

	return context, nil
}

// validateEncryptionConfig validates an EncryptionConfig struct to ensure that if encryption
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
			return fmt.Errorf("%w: %w", ErrInvalidAESKey, err)
		}

		// Validate the fallback AES Key
		if encryptionConfig.AESKeyFallback != nil {
			_, err = aes.NewCipher(encryptionConfig.AESKeyFallback)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidAESKey, err)
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

// ResolveTemplate accepts a map marshaled as JSON or YAML. It also accepts a struct
// with string fields that will be made available when the template is processed.
// For example, if the argument is `struct{ClusterName string}{"cluster1"}`,
// the value `cluster1` would be available with `{{ .ClusterName }}`. This can
// also be `nil` if no fields should be made available.
//
// ResolveTemplate will process any template strings in the map and return the processed map. The
// ErrMissingAPIResource is returned when one or more "lookup" calls referenced an API resource
// which isn't installed on the Kubernetes API server.
//
// The input options contains options for template resolution. The options.Watcher field is an ObjectIdentifier that is
// used in caching mode and the controller-runtime integration. Set this to nil when not in caching mode. When in
// caching mode, watches are automatically garbage collected when a new call to ResolveTemplate no longer specifies an
// object or list query it used to.
//
// This method is only concurrency safe when caching is enabled. When caching is disabled, a local cache of objects
// is stored just for the ResolveTemplate execution to avoid duplicate API queries. If running this method concurrently
// with caching disabled, you may get some items from the temporary cache while others will be from API queries.
func (t *TemplateResolver) ResolveTemplate(
	tmplRaw []byte, context interface{}, options *ResolveOptions,
) (TemplateResult, error) {
	klog.V(2).Infof("ResolveTemplate for: %v", string(tmplRaw))

	if options == nil {
		options = &ResolveOptions{}
	}

	var resolvedResult TemplateResult

	err := validateEncryptionConfig(options.EncryptionConfig)
	if err != nil {
		return resolvedResult, fmt.Errorf("error validating EncryptionConfig: %w", err)
	}

	if t.dynamicWatcher != nil {
		if options.Watcher == nil {
			return resolvedResult, fmt.Errorf(
				"%w: options.Watcher cannot be nil if caching is enabled",
				ErrInvalidInput,
			)
		}
	} else if len(options.ContextTransformers) != 0 {
		return resolvedResult, fmt.Errorf(
			"%w: options.ContextTransformers cannot be set if caching is disabled",
			ErrInvalidInput,
		)
	}

	ctx, err := getValidContext(context)
	if err != nil {
		return resolvedResult, err
	}

	// Build Map of supported template functions
	funcMap := template.FuncMap{
		"copyConfigMapData": t.copyConfigMapDataHelper(options),
		"copySecretData":    t.copySecretDataHelper(options, &resolvedResult),
		"fromSecret":        t.fromSecretHelper(options, &resolvedResult),
		"fromConfigMap":     t.fromConfigMapHelper(options),
		"fromClusterClaim":  t.fromClusterClaimHelper(options),
		"lookup":            t.lookupHelper(options, &resolvedResult),
		"base64enc":         base64encode,
		"base64dec":         base64decode,
		"b64enc":            base64encode, // Link the Sprig name to our function
		"b64dec":            base64decode, // Link the Sprig name to our function
		"autoindent":        autoindent,
		"indent":            t.indent,
		"atoi":              atoi,
		"toInt":             toInt,
		"toBool":            toBool,
		"toLiteral":         toLiteral,
	}

	// Add all the functions from sprig we will support
	for _, fname := range exportedSprigFunctions {
		funcMap[fname] = getSprigFunc(fname)
	}

	if options.EncryptionEnabled {
		funcMap["fromSecret"] = t.fromSecretProtectedHelper(options, &resolvedResult)
		funcMap["protect"] = t.protectHelper(options)
		funcMap["copySecretData"] = t.copySecretDataProtectedHelper(options, &resolvedResult)
	} else {
		// In other encryption modes, return a readable error if the protect template function is accidentally used.
		funcMap["protect"] = func(s string) (string, error) { return "", ErrProtectNotEnabled }
	}

	for _, funcName := range t.config.DisabledFunctions {
		delete(funcMap, funcName)
	}

	// create template processor and Initialize function map
	tmpl := template.New("tmpl").Delims(t.config.StartDelim, t.config.StopDelim).Funcs(funcMap)

	// convert the JSON to YAML if necessary
	var templateStr string

	if !options.InputIsYAML {
		templateYAMLBytes, err := JSONToYAML(tmplRaw)
		if err != nil {
			return resolvedResult, fmt.Errorf("failed to convert the policy template to YAML: %w", err)
		}

		templateStr = string(templateYAMLBytes)
	} else {
		templateStr = string(tmplRaw)
	}

	klog.V(2).Infof("Initial template str to resolve : %v ", templateStr)

	if options.DecryptionEnabled {
		templateStr, err = t.processEncryptedStrs(options, &resolvedResult, templateStr)
		if err != nil {
			return resolvedResult, err
		}
	}

	// processForDataTypes handles scenarios where quotes need to be removed for
	// special data types or cases where multiple values are returned
	templateStr = t.processForDataTypes(templateStr)

	// convert `autoindent` placeholders to `indent N`
	if strings.Contains(templateStr, "autoindent") {
		templateStr = t.processForAutoIndent(templateStr)
	}

	tmpl, err = tmpl.Parse(templateStr)
	if err != nil {
		tmplRawStr := string(tmplRaw)
		klog.Errorf(
			"error parsing template string %v,\n template str %v,\n error: %v", tmplRawStr, templateStr, err,
		)

		return resolvedResult, fmt.Errorf("failed to parse the template JSON string %v: %w", tmplRawStr, err)
	}

	var buf bytes.Buffer

	// If the dynamic watcher caching style is disabled, clear the cache after resolving the template.
	if t.tempCallCache != nil {
		defer t.tempCallCache.Clear()
	}

	if t.dynamicWatcher != nil {
		watcher := *options.Watcher

		if !t.skipBatchManagement {
			err := t.dynamicWatcher.StartQueryBatch(watcher)
			if err != nil {
				if !errors.Is(err, client.ErrQueryBatchInProgress) {
					return resolvedResult, err
				}

				if !options.DisableAutoCacheCleanUp {
					return resolvedResult, fmt.Errorf(
						"ResolveTemplate cannot be called with the same watchedObject in parallel: %w", err,
					)
				}
			}

			if options.DisableAutoCacheCleanUp {
				resolvedResult.CacheCleanUp = func() error {
					return t.dynamicWatcher.EndQueryBatch(*options.Watcher)
				}
			} else {
				defer func() {
					err := t.dynamicWatcher.EndQueryBatch(watcher)
					if err != nil && !errors.Is(err, client.ErrQueryBatchNotStarted) {
						klog.Errorf("failed to end the query batch for %s: %v", watcher, err)
					}
				}()
			}
		}

		for i, contextTransformer := range options.ContextTransformers {
			var err error

			queryObj := cachingQueryAPI{dynamicWatcher: t.dynamicWatcher, watcher: *options.Watcher}

			ctx, err = contextTransformer(&queryObj, context)
			if err != nil {
				return resolvedResult, fmt.Errorf(
					"%w at options.ContextTransformers[%d]: %w", ErrContextTransformerFailed, i, err,
				)
			}
		}
	}

	err = tmpl.Execute(&buf, ctx)
	if err != nil {
		tmplRawStr := string(tmplRaw)
		klog.Errorf("error resolving the template %v,\n template str %v,\n error: %v", tmplRawStr, templateStr, err)

		return resolvedResult, fmt.Errorf("failed to resolve the template %v: %w", tmplRawStr, err)
	}

	resolvedTemplateStr := buf.String()
	klog.V(3).Infof("resolved template str: %v ", resolvedTemplateStr)
	// unmarshall before returning

	resolvedTemplateBytes, err := yamlToJSON(buf.Bytes())
	if err != nil {
		return resolvedResult, fmt.Errorf("failed to convert the resolved template to JSON: %w", err)
	}

	resolvedResult.ResolvedJSON = resolvedTemplateBytes

	return resolvedResult, nil
}

// UncacheWatcher will clear the watcher from the cache and remove all associated API watches.
func (t *TemplateResolver) UncacheWatcher(watcher client.ObjectIdentifier) error {
	if t.dynamicWatcher == nil {
		return ErrCacheDisabled
	}

	return t.dynamicWatcher.RemoveWatcher(watcher)
}

// ListWatchedFromCache will return all watched objects by the watcher in the cache. The ErrNoCacheEntry error is
// returned if no template function has caused an entry to be cached.
func (t *TemplateResolver) ListWatchedFromCache(watcher client.ObjectIdentifier) ([]unstructured.Unstructured, error) {
	if t.dynamicWatcher == nil {
		return nil, ErrCacheDisabled
	}

	return t.dynamicWatcher.ListWatchedFromCache(watcher)
}

// GetFromCache will return the object from the cache. The ErrNoCacheEntry error is returned if no template function
// has caused an entry to be cached.
func (t *TemplateResolver) GetFromCache(
	gvk schema.GroupVersionKind, namespace string, name string,
) (*unstructured.Unstructured, error) {
	if t.dynamicWatcher == nil {
		return nil, ErrCacheDisabled
	}

	return t.dynamicWatcher.GetFromCache(gvk, namespace, name)
}

// GetWatchCount returns the total number of active API watch requests which can be used for metrics.
func (t *TemplateResolver) GetWatchCount() uint {
	if t.dynamicWatcher != nil {
		return t.dynamicWatcher.GetWatchCount()
	}

	return 0
}

//nolint:wsl
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
	//nolint: lll
	expression := `:\s+(?:[\|>]-?\s+)?(?:'?\s*)(` + d1 + `(?:.*\|\s*(?:toInt|toBool|toLiteral)|(?:.*(?:copyConfigMapData|copySecretData))).*` + d2 + `)(?:\s*'?)`
	re := regexp.MustCompile(expression)
	klog.V(2).Infof("\n Pattern: %v\n", re.String())

	submatchall := re.FindAllStringSubmatch(str, -1)
	if submatchall == nil {
		return str
	}
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

// JSONToYAML converts JSON to YAML using yaml.v3. This is important since
// line wrapping is disabled in v3.
func JSONToYAML(j []byte) ([]byte, error) {
	// Convert the JSON to an object
	var jsonObj interface{}

	err := yaml.Unmarshal(j, &jsonObj)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}

	// Marshal this object into YAML
	var b bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&b)
	yamlEncoder.SetIndent(yamlIndentation)

	err = yamlEncoder.Encode(&jsonObj)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}

	return b.Bytes(), nil
}

// yamlToJSON converts YAML to JSON.
func yamlToJSON(y []byte) ([]byte, error) {
	// Convert the YAML to an object.
	var yamlObj interface{}

	err := yaml.Unmarshal(y, &yamlObj)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}

	// Convert this object to JSON
	return json.Marshal(yamlObj) //nolint:wrapcheck
}

func (t *TemplateResolver) indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces+int(t.config.AdditionalIndentation))
	npad := "\n" + pad + strings.Replace(v, "\n", "\n"+pad, -1)

	return strings.TrimSpace(npad)
}

// This is so that the user gets a nicer error in the event some valid scenario slips through the
// regex.
func autoindent(_ string) (string, error) {
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

// CachingQueryAPI is a limited query API that will cache results. This is used with ContextTransformers.
type CachingQueryAPI interface {
	// Get will add an additional watch and return the watched object.
	Get(
		gvk schema.GroupVersionKind, namespace string, name string,
	) (*unstructured.Unstructured, error)
	// List will add an additional list watch and return the watched objects.
	List(
		gvk schema.GroupVersionKind, namespace string, selector labels.Selector,
	) ([]unstructured.Unstructured, error)
}

type cachingQueryAPI struct {
	dynamicWatcher client.DynamicWatcher
	watcher        client.ObjectIdentifier
}

func (c *cachingQueryAPI) Get(
	gvk schema.GroupVersionKind, namespace string, name string,
) (*unstructured.Unstructured, error) {
	return c.dynamicWatcher.Get(c.watcher, gvk, namespace, name)
}

func (c *cachingQueryAPI) List(
	gvk schema.GroupVersionKind, namespace string, selector labels.Selector,
) ([]unstructured.Unstructured, error) {
	return c.dynamicWatcher.List(c.watcher, gvk, namespace, selector)
}
