// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/golang/glog"
	"github.com/spf13/cast"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

const (
	defaultStartDelim = "{{"
	defaultStopDelim  = "}}"
	glogDefLvl        = 2
)

// Config is a struct containing configuration for the API. Some are required.
//
// - KubeAPIResourceList sets the cache for the Kubernetes API resources. If this is
// set, template processing will not try to rediscover the Kubernetes API resources
// needed for dynamic client/ GVK lookups.
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
	KubeAPIResourceList []*metav1.APIResourceList
	LookupNamespace     string
	StartDelim          string
	StopDelim           string
}

// TemplateResolver is the API for processing templates. It's better to use the NewResolver function
// instead of instantiating this directly so that configuration defaults and validation are applied.
type TemplateResolver struct {
	// Required
	kubeClient *kubernetes.Interface
	// Optional
	kubeAPIResourceList []*metav1.APIResourceList
	kubeConfig          *rest.Config
	lookupNamespace     string
	startDelim          string
	stopDelim           string
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

	if (config.StartDelim != "" && config.StopDelim == "") || (config.StartDelim == "" && config.StopDelim != "") {
		return nil, fmt.Errorf("the configurations StartDelim and StopDelim cannot be set independently")
	}

	startDelim := defaultStartDelim
	stopDelim := defaultStopDelim
	// It's only required to check config.StartDelim since it's invalid to set these independently
	if config.StartDelim != "" {
		startDelim = config.StartDelim
		stopDelim = config.StopDelim
	}
	glog.V(glogDefLvl).Infof("Using the delimiters of %s and %s", startDelim, stopDelim)

	return &TemplateResolver{
		// Required
		kubeClient: kubeClient,
		kubeConfig: kubeConfig,
		// Optional
		kubeAPIResourceList: config.KubeAPIResourceList,
		lookupNamespace:     config.LookupNamespace,
		startDelim:          startDelim,
		stopDelim:           stopDelim,
	}, nil
}

// HasTemplate performs a simple check for the template start delimiter to
// indicate if the input string has a template. If the startDelim argument is
// an empty string, the default start delimiter of "{{" will be used.
func HasTemplate(templateStr string, startDelim string) bool {
	if startDelim == "" {
		startDelim = defaultStartDelim
	}

	glog.V(glogDefLvl).Infof("HasTemplate template str:  %v", templateStr)
	glog.V(glogDefLvl).Infof("Checking for the start delimiter:  %s", startDelim)

	hasTemplate := false
	if strings.Contains(templateStr, startDelim) {
		hasTemplate = true
	}

	glog.V(glogDefLvl).Infof("hasTemplate: %v", hasTemplate)

	return hasTemplate
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

// ResolveTemplate accepts an unmarshaled map that can be marshaled to YAML.
// it also accepts a struct with string fields that will be made available
// when the template is processed. For example, if the argument is
// `struct{ClusterName string}{"cluster1"}`, the value `cluster1` would be
// available with `{{ .ClusterName }}`. This can also be `nil` if no fields
// should be made available.
//
// ResolveTemplate will process any template strings in the map and return the processed map.
func (t *TemplateResolver) ResolveTemplate(tmplMap interface{}, context interface{}) (interface{}, error) {
	glog.V(glogDefLvl).Infof("ResolveTemplate for: %v", tmplMap)

	ctx, err := getValidContext(context)
	if err != nil {
		return "", err
	}

	// Build Map of supported template functions
	funcMap := template.FuncMap{
		"fromSecret":       t.fromSecret,
		"fromConfigMap":    t.fromConfigMap,
		"fromClusterClaim": t.fromClusterClaim,
		"lookup":           t.lookup,
		"base64enc":        base64encode,
		"base64dec":        base64decode,
		"indent":           indent,
		"atoi":             atoi,
		"toInt":            toInt,
		"toBool":           toBool,
	}

	// create template processor and Initialize function map
	tmpl := template.New("tmpl").Delims(t.startDelim, t.stopDelim).Funcs(funcMap)

	// convert the interface to yaml to string
	// ext.raw is jsonMarshalled data which the template processor is not accepting
	// so marshaling  unmarshaled(ext.raw) to yaml to string

	templateStr, err := toYAML(tmplMap)
	if err != nil {
		return "", fmt.Errorf("failed to convert the policy template to yaml: %w", err)
	}
	glog.V(glogDefLvl).Infof("Initial template str to resolve : %v ", templateStr)

	// process for int or bool
	if strings.Contains(templateStr, "toInt") || strings.Contains(templateStr, "toBool") {
		templateStr = t.processForDataTypes(templateStr)
	}

	tmpl, err = tmpl.Parse(templateStr)
	if err != nil {
		glog.Errorf("error parsing template map %v,\n template str %v,\n error: %v", tmplMap, templateStr, err)

		return "", fmt.Errorf("failed to parse the template map %v: %w", tmplMap, err)
	}

	var buf strings.Builder
	err = tmpl.Execute(&buf, ctx)
	if err != nil {
		glog.Errorf("error executing the template map %v,\n template str %v,\n error: %v", tmplMap, templateStr, err)

		return "", fmt.Errorf("failed to execute the template map %v: %w", tmplMap, err)
	}

	resolvedTemplateStr := buf.String()
	glog.V(glogDefLvl).Infof("resolved template str: %v ", resolvedTemplateStr)
	// unmarshall before returning

	resolvedTemplateIntf, err := fromYAML(resolvedTemplateStr)
	if err != nil {
		return "", fmt.Errorf("failed to convert the resolved template back to YAML: %w", err)
	}

	return resolvedTemplateIntf, nil
}

// fromYAML converts a YAML document into a map[string]interface{}.
func fromYAML(str string) (map[string]interface{}, error) {
	m := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(str), &m); err != nil {
		glog.Errorf("error parsing the YAML the template str %v , \n %v ", str, err)

		return m, fmt.Errorf("failed to parse the YAML template str: %w", err)
	}

	return m, nil
}

// toYAML converts a map[string]interface{} to a YAML document string.
func toYAML(v interface{}) (string, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		glog.Errorf("error parsing the YAML template map %v , \n %v ", v, err)

		return "", fmt.Errorf("failed to parse the YAML template map %v: %w", v, err)
	}

	return strings.TrimSuffix(string(data), "\n"), nil
}

func (t *TemplateResolver) processForDataTypes(str string) string {
	// the idea is to remove the quotes enclosing the template if it ends in toBool ot ToInt
	// quotes around the resolved template forces the value to be a string..
	// so removal of these quotes allows yaml to process the datatype correctly..

	// the below pattern searches for optional block scalars | or >.. followed by the quoted template ,
	// and replaces it with just the template txt thats inside in the quotes
	// ex-1 key : "{{ "6" | toInt }}"  .. is replaced with  key : {{ "6" | toInt }}
	// ex-2 key : |
	//						"{{ "true" | toBool }}" .. is replaced with key : {{ "true" | toBool }}
	d1 := regexp.QuoteMeta(t.startDelim)
	d2 := regexp.QuoteMeta(t.stopDelim)
	re := regexp.MustCompile(`:\s+(?:[\|>][-]?\s+)?(?:['|"]\s*)?(` + d1 + `.*?\s+\|\s+(?:toInt|toBool)\s*` + d2 + `)(?:\s*['|"])?`)
	glog.V(glogDefLvl).Infof("\n Pattern: %v\n", re.String())

	submatchall := re.FindAllStringSubmatch(str, -1)
	glog.V(glogDefLvl).Infof("\n All Submatches:\n%v", submatchall)

	processeddata := re.ReplaceAllString(str, ": $1")
	glog.V(glogDefLvl).Infof("\n processed data :\n%v", processeddata)

	return processeddata
}

func indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces)
	npad := "\n" + pad + strings.Replace(v, "\n", "\n"+pad, -1)

	return strings.TrimSpace(npad)
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
