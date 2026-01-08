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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
		"the input context must be a struct that recurses to kinds bool, int, float, or string",
	)
	ErrMissingNamespace = errors.New(
		"the lookup of a single namespaced resource must have a namespace specified",
	)
	ErrRestrictedNamespace      = errors.New("the namespace argument is restricted")
	ErrInvalidInput             = errors.New("the input is invalid")
	ErrDenylistedFunctionUsed   = errors.New("use of denylisted template function")
	ErrCacheDisabled            = client.ErrCacheDisabled
	ErrNoCacheEntry             = client.ErrNoCacheEntry
	ErrContextTransformerFailed = errors.New("the context transformer failed")
)

// sensitiveSprigFunctions lists Sprig helper functions that are considered high
// risk or non-deterministic in this environment. These are always denylisted.
var sensitiveSprigFunctions = []string{
	"env",
	"expandenv",
}

// isSensitiveSprigFunction returns true if the given function name is
// considered high risk and should always be denylisted.
func isSensitiveSprigFunction(name string) bool {
	for _, fn := range sensitiveSprigFunctions {
		if fn == name {
			return true
		}
	}

	return false
}

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
//
// - SkipBatchManagement can be set if multiple calls to ResolveTemplate are needed for one watcher before API watches
// and cache entries are cleaned up. The manual control is done with the StartQueryBatch and EndQueryBatch methods.
// This has no effect if caching is not enabled.
type Config struct {
	AdditionalIndentation      uint32
	DisabledFunctions          []string
	StartDelim                 string
	StopDelim                  string
	MissingAPIResourceCacheTTL time.Duration
	SkipBatchManagement        bool
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
// - CustomFunctions is an optional map of custom functions available during template resolution.
//
//   - DenylistFunctions is an optional list of template function names that are denylisted for this
//     resolution call. Any attempt to invoke one of these functions in a template will result in a
//     resolution error.
//
// - EncryptionConfig is the configuration for template encryption/decryption functionality.
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
	CustomFunctions        template.FuncMap
	EncryptionConfig
	DenylistFunctions []string
	InputIsYAML       bool
	LookupNamespace   string
	Watcher           *client.ObjectIdentifier
}

type TemplateContext struct {
	ObjectNamespace string
	ObjectName      string
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

type UsedResource struct {
	Resource unstructured.Unstructured
	// If the used resource was fetched locally through a user-provided YAML file or remotely through kubeconfig
	IsRemote bool
}

// TemplateResolver is the API for processing templates. It's better to use the NewResolver function
// instead of instantiating this directly so that configuration defaults and validation are applied.
type TemplateResolver struct {
	config Config
	// Used when caching is disabled.
	dynamicClient dynamic.Interface
	// Used when instantiated with NewResolverWithCaching. This will create watches and the cache will get
	// automatically updated.
	dynamicWatcher client.DynamicWatcher
	// If caching is disabled, this will act as a temporary cache for objects during the execution of the
	// ResolveTemplate call.
	tempCallCache client.ObjectCache
	// Use in template resolver CLI to create outputs including resources used in fromSecret, fromConfigMap,
	// loopUp etc functions.
	usedResources []UsedResource
	// Used in template resolver CLI to use locally provided resources during rendering
	localResources []unstructured.Unstructured
}

type TemplateResult struct {
	ResolvedJSON []byte
	// HasSensitiveData is true if a template references a secret or decrypts an encrypted value.
	HasSensitiveData bool
}

// NewResolver creates a new (non-caching) TemplateResolver instance without using localResources, which is the API for processing templates.
//
// - kubeConfig is the rest.Config instance used to create Kubernetes clients for template processing.
//
// - config is the Config instance for configuring optional values for template processing.
func NewResolver(kubeConfig *rest.Config, config Config) (*TemplateResolver, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	return NewResolverWithClients(dynamicClient, discoveryClient, config, make([]unstructured.Unstructured, 0))
}

// NewResolverWithLocalResources creates a new (non-caching) TemplateResolver instance with local resources
// Very similar to NewResolver with simply the addition of local resources
// This is mainly used for the template-resolver cli
func NewResolverWithLocalResources(kubeConfig *rest.Config, config Config, localResources []unstructured.Unstructured) (*TemplateResolver, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	return NewResolverWithClients(dynamicClient, discoveryClient, config, localResources)
}

// NewResolverWithClients creates a new (non-caching) TemplateResolver instance, which is the API for processing
// templates.
func NewResolverWithClients(
	dynamicClient dynamic.Interface,
	discoveryClient discovery.DiscoveryInterface,
	config Config,
	localResources []unstructured.Unstructured,
) (*TemplateResolver, error) {
	if (config.StartDelim != "" && config.StopDelim == "") || (config.StartDelim == "" && config.StopDelim != "") {
		return nil, errors.New("the configurations StartDelim and StopDelim cannot be set independently")
	}

	// It's only required to check config.StartDelim since it's invalid to set these independently
	if config.StartDelim == "" {
		config.StartDelim = defaultStartDelim
		config.StopDelim = defaultStopDelim
	}

	klog.V(2).Infof("Using the delimiters of %s and %s", config.StartDelim, config.StopDelim)

	tempCallCache := client.NewObjectCache(
		// Set the missing API resource cache TTL in this mode because the cache just lives for the ResolveTemplate
		// execution and duplicate queries when a CRD is missing is not necessary.
		discoveryClient, client.ObjectCacheOptions{
			MissingAPIResourceCacheTTL: time.Minute,
			UnsafeDisableDeepCopy:      false,
		},
	)

	return &TemplateResolver{
		config:         config,
		dynamicClient:  dynamicClient,
		dynamicWatcher: nil,
		tempCallCache:  tempCallCache,
		localResources: localResources,
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
	*TemplateResolver, source.TypedSource[reconcile.Request], error,
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
		return nil, errors.New("the configurations StartDelim and StopDelim cannot be set independently")
	}

	// It's only required to check config.StartDelim since it's invalid to set these independently
	if config.StartDelim == "" {
		config.StartDelim = defaultStartDelim
		config.StopDelim = defaultStopDelim
	}

	return &TemplateResolver{
		config:         config,
		dynamicClient:  nil,
		dynamicWatcher: dynWatcher,
		tempCallCache:  nil,
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
	re := regexp.MustCompile(d1 + `-?(\s*fromSecret\s+.*|\s*copySecretData\s+.*|.*\|\s*protect\s*)-?` + d2)
	usesEncryption := re.MatchString(templateStr)

	klog.V(2).Infof("usesEncryption: %v", usesEncryption)

	return usesEncryption
}

// getValidContext takes an input context struct and validates it. If it is valid, the context will be returned as is.
// If the input context is nil, an empty struct will be returned. If it's not valid, an error will be returned.
func getValidContext(value interface{}) (interface{}, error) {
	if value == nil {
		return struct{}{}, nil
	}

	// Require the context to be a struct
	if reflect.TypeOf(value).Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w, but found a parent of kind %s", ErrInvalidContextType, reflect.TypeOf(value))
	}

	// Require the context to recurse to primitive keys and values through structs, maps, or arrays
	err := getValidContextHelper(value)
	if err != nil {
		return nil, err
	}

	return value, nil
}

// isPrimitive detects primitive types from the reflect package
func isPrimitive(kind reflect.Kind) bool {
	switch kind {
	case
		reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true

	default:
		return false
	}
}

func getValidContextHelper(value any) error {
	f := reflect.TypeOf(value)
	if f == nil { // nil interface value.
		return nil
	}

	// Allow primitive types (excludes complex numbers)
	if isPrimitive(f.Kind()) {
		return nil
	}

	// Handle complex types
	switch f.Kind() {
	// Allow arrays and recurse into each item
	case reflect.Slice, reflect.Array:
		// Iterate over embedded maps and interfaces
		if f.Elem().Kind() == reflect.Interface || f.Elem().Kind() == reflect.Map {
			for i := range reflect.ValueOf(value).Len() {
				err := getValidContextHelper(reflect.ValueOf(value).Index(i).Interface())
				if err != nil {
					return err
				}
			}
		} else if !isPrimitive(f.Elem().Kind()) {
			// Verify map values are primitive
			return fmt.Errorf("%w, found an array with values of kind %s", ErrInvalidContextType, reflect.TypeOf(value))
		}

	// Allow structs and recurse into fields
	case reflect.Struct:
		for i := range f.NumField() {
			// Only handle exported struct fields (causes a panic calling Interface())
			if f.Field(i).IsExported() {
				err := getValidContextHelper(reflect.ValueOf(value).Field(i).Interface())
				if err != nil {
					return err
				}
			}
		}

	// Allow maps and recurse into keys
	case reflect.Map:
		// Only allow primitive keys in maps
		if !isPrimitive(f.Key().Kind()) {
			return fmt.Errorf("%w, found a map with keys of kind %s", ErrInvalidContextType, f.Key().Kind())
		}

		// Iterate over embedded maps and interfaces
		if f.Elem().Kind() == reflect.Interface || f.Elem().Kind() == reflect.Map {
			for _, key := range reflect.ValueOf(value).MapKeys() {
				err := getValidContextHelper(reflect.ValueOf(value).MapIndex(key).Interface())
				if err != nil {
					return err
				}
			}
		} else if !isPrimitive(f.Elem().Kind()) {
			// Verify map values are primitive
			return fmt.Errorf("%w, found a map with values of kind %s", ErrInvalidContextType, reflect.TypeOf(value))
		}

	// Disallow all else like pointers and channels
	default:
		return fmt.Errorf("%w, found value of kind %s", ErrInvalidContextType, reflect.TypeOf(value))
	}

	return nil
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

// StartQueryBatch will start a query batch transaction for the watcher. After template resolution is complete for a
// watcher, calling EndQueryBatch will clean up the non-applicable preexisting watches made from before this query
// batch.
func (t *TemplateResolver) StartQueryBatch(watcher client.ObjectIdentifier) error {
	if t.dynamicWatcher == nil {
		return ErrCacheDisabled
	}

	if !t.config.SkipBatchManagement {
		return errors.New(
			"the TemplateResolver must have SkipBatchManagement set to true to manage the batches explicitly",
		)
	}

	return t.dynamicWatcher.StartQueryBatch(watcher)
}

// EndQueryBatch will stop a query batch transaction for the watcher. This will clean up the non-applicable preexisting
// watches made from before this query batch.
func (t *TemplateResolver) EndQueryBatch(watcher client.ObjectIdentifier) error {
	if t.dynamicWatcher == nil {
		return ErrCacheDisabled
	}

	return t.dynamicWatcher.EndQueryBatch(watcher)
}

// ResolveTemplate accepts a map marshaled as JSON or YAML. It also accepts a combination of structs and maps that
// ultimately end in a string value to be made available when the template is processed.
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

	// Always denylist sensitive functions that can expose host information.
	if options.DenylistFunctions == nil {
		options.DenylistFunctions = []string{}
	}

	options.DenylistFunctions = append(options.DenylistFunctions, sensitiveSprigFunctions...)

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
		"copyConfigMapData":      t.copyConfigMapDataHelper(options),
		"copySecretData":         t.copySecretDataHelper(options, &resolvedResult),
		"fromSecret":             t.fromSecretHelper(options, &resolvedResult),
		"fromConfigMap":          t.fromConfigMapHelper(options),
		"fromClusterClaim":       t.fromClusterClaimHelper(options),
		"lookupClusterClaim":     t.lookupClusterClaimHelper(options),
		"getNodesWithExactRoles": t.getNodesWithExactRolesHelper(options, &resolvedResult),
		"hasNodesWithExactRoles": t.hasNodesWithExactRolesHelper(options),
		"lookup":                 t.lookupHelper(options, &resolvedResult),
		"base64enc":              base64encode,
		"base64dec":              base64decode,
		"b64enc":                 base64encode, // Link the Sprig name to our function
		"b64dec":                 base64decode, // Link the Sprig name to our function
		"autoindent":             autoindent,
		"indent":                 t.indent,
		"atoi":                   atoi,
		"toInt":                  toInt,
		"toBool":                 toBool,
		"toLiteral":              toLiteral,
		"fromJSON":               getSprigFunc("fromJson"),      // Link uppercase invocation to JSON parser
		"mustFromJSON":           getSprigFunc("mustFromJson"),  // Link uppercase invocation to JSON parser
		"toJSON":                 getSprigFunc("toJson"),        // Link uppercase invocation to JSON parser
		"mustToJSON":             getSprigFunc("mustToJson"),    // Link uppercase invocation to JSON parser
		"toRawJSON":              getSprigFunc("toRawJson"),     // Link uppercase invocation to JSON parser
		"mustToRawJSON":          getSprigFunc("mustToRawJson"), // Link uppercase invocation to JSON parser
		"fromYAML":               fromYAML,
		"toYAML":                 toYAML,
		"fromYaml":               fromYAML, // Link lowercase invocation to YAML parser
		"toYaml":                 toYAML,   // Link lowercase invocation to YAML parser
	}

	// Add all the functions from Sprig we will support. If a function name is already
	// present in the funcMap (for example, when we override Sprig behavior locally),
	// keep the existing implementation.
	for fname, f := range sprigFuncMap {
		if _, exists := funcMap[fname]; exists {
			continue
		}

		funcMap[fname] = f
	}

	if options.EncryptionEnabled {
		funcMap["fromSecret"] = t.fromSecretProtectedHelper(options, &resolvedResult)
		funcMap["protect"] = t.protectHelper(options)
		funcMap["copySecretData"] = t.copySecretDataProtectedHelper(options, &resolvedResult)
	} else {
		// In other encryption modes, return a readable error if the protect template function is accidentally used.
		funcMap["protect"] = func(_ string) (string, error) { return "", ErrProtectNotEnabled }
	}

	for _, funcName := range t.config.DisabledFunctions {
		delete(funcMap, funcName)
	}

	for customFuncName, customFunc := range options.CustomFunctions {
		funcMap[customFuncName] = customFunc
	}

	// Wrap any denylisted functions so that using them results in a clear error at
	// template execution time instead of an undefined function error.
	if len(options.DenylistFunctions) != 0 {
		for _, name := range options.DenylistFunctions {
			funcMap[name] = func(_ ...interface{}) (interface{}, error) {
				if isSensitiveSprigFunction(name) {
					return nil, fmt.Errorf(
						"%w: function '%s' is considered a security risk",
						ErrDenylistedFunctionUsed,
						name,
					)
				}

				return nil, fmt.Errorf("%w: function '%s' is not allowed", ErrDenylistedFunctionUsed, name)
			}
		}
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

		if !t.config.SkipBatchManagement {
			err := t.dynamicWatcher.StartQueryBatch(watcher)
			if err != nil {
				if !errors.Is(err, client.ErrQueryBatchInProgress) {
					return resolvedResult, err
				}

				return resolvedResult, fmt.Errorf(
					"ResolveTemplate cannot be called with the same watchedObject in parallel: %w", err,
				)
			}

			defer func() {
				err := t.dynamicWatcher.EndQueryBatch(watcher)
				if err != nil {
					klog.Errorf("failed to end the query batch for %s: %v", watcher, err)
				}
			}()
		}

		ctx, err = t.applyContextTransformers(context, options)
		if err != nil {
			return resolvedResult, err
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
	// unmarshal before returning
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
	expression := `:\s+(?:[\|>]-?\s+)?(?:'?\s*)(` + d1 + `-?(?:.*\|\s*(?:toInt|toBool|toLiteral)|(?:.*(?:copyConfigMapData|copySecretData))).*` + d2 + `)(?:\s*'?)`
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
	re := regexp.MustCompile(`( *)(?:'|")?(` + d1 + `.*\| *autoindent *-?` + d2 + `)`)
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

// errRecoverPanic catches a Go panic and formats the returned error.
func errRecoverPanic(err *error, msg string) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s: %v", msg, r)
	}
}

// toYAML takes an interface, marshals it to YAML, and returns a string.
//
// The output is unindented, so output must be piped to `autoindent` or
// `indent N` if indentation is needed.
func toYAML(v any) (str string, err error) {
	// Adding a recover here because the marshal has the potential to panic on errors
	defer errRecoverPanic(&err, "yaml marshal error")

	var data bytes.Buffer
	encoder := yaml.NewEncoder(&data)
	encoder.SetIndent(yamlIndentation)

	err = encoder.Encode(v)
	if err != nil {
		return str, err
	}

	// goyaml.v3 has some odd behaviors around newlines that arise because we
	// marshal/unmarshal in the yamlToJSON function. My observations were:
	// - If there is a trailing newline, it considers it multiline and leaves the newlines alone.
	// - If there is no trailing newline, it converts any newline into a space.
	//   (This includes when users use the YAML chomping syntax '|-')
	//
	// The "no trailing newline" issue can be resolved by replacing \n with \n\n
	// when there's no trailing newline, but that brings additional complexities
	// since the trailing newline is external to the template and can't be
	// reliably detected, so it seems it's better to document as a known issue
	// that chomping shouldn't be used.
	str = strings.TrimSuffix(data.String(), "\n")

	return str, err
}

// fromYAML converts a YAML document into an interface{}.
func fromYAML(str string) (m any, err error) {
	err = yaml.Unmarshal([]byte(str), &m)

	return m, err
}

func (t *TemplateResolver) indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces+int(t.config.AdditionalIndentation))
	npad := "\n" + pad + strings.ReplaceAll(v, "\n", "\n"+pad)

	return strings.TrimSpace(npad)
}

// Avoid duplicate entries since operatorPolicy calls ProcessTemplate multiple times
// for the same objectTemplate.
func (t *TemplateResolver) appendUsedResources(input unstructured.Unstructured, isRemote bool) {
	for _, res := range t.usedResources {
		if reflect.DeepEqual(res.Resource, input) { // Keep only non-matching elements
			return // Resource already exists, no need to append
		}
	}
	t.usedResources = append(t.usedResources, UsedResource{Resource: input, IsRemote: isRemote})
}

func (t *TemplateResolver) GetUsedResources() []UsedResource {
	return t.usedResources
}

// applyContextTransformers runs the configured ContextTransformers in order and
// returns the final transformed context. This is only called when a
// DynamicWatcher is configured.
func (t *TemplateResolver) applyContextTransformers(
	origContext interface{},
	options *ResolveOptions,
) (interface{}, error) {
	ctx := origContext
	var err error

	for i, contextTransformer := range options.ContextTransformers {
		queryObj := cachingQueryAPI{dynamicWatcher: t.dynamicWatcher, watcher: *options.Watcher}

		ctx, err = contextTransformer(&queryObj, ctx)
		if err != nil {
			return nil, fmt.Errorf(
				"%w at options.ContextTransformers[%d]: %w", ErrContextTransformerFailed, i, err,
			)
		}
	}

	return ctx, nil
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
