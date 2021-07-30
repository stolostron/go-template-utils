// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"fmt"
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
	glogDefLvl = 2
)

// Config is a struct containing configuration for the API. Some are required.
//
// - KubeAPIResourceList sets the cache for the Kubernetes API resources. If this is
// set, template processing will not try to rediscover the Kubernetes API resources
// needed for dynamic client/ GVK lookups. If this is not set, KubeConfig must be set.
//
// - KubeConfig the configuration of the Kubernetes cluster the template is running against. If this
// is not set, then KubeAPIResourceList must be set.
//
type Config struct {
	KubeAPIResourceList []*metav1.APIResourceList
	KubeConfig          *rest.Config
}

// TemplateResolver is the API for processing templates. It's better to use the NewResolver function
// instead of instantiating this directly so that configuration defaults and validation are applied.
type TemplateResolver struct {
	// Required
	kubeClient *kubernetes.Interface
	// Optional
	kubeAPIResourceList []*metav1.APIResourceList
	kubeConfig          *rest.Config
}

// NewResolver creates a new TemplateResolver instance, which is the API for processing templates.
//
// - kubeClient is the Kubernetes client to be used for the template lookup functions.
//
// - config is the Config instance for configuration for template processing.
func NewResolver(kubeClient *kubernetes.Interface, config Config) (*TemplateResolver, error) {
	if kubeClient == nil {
		return nil, fmt.Errorf("kubeClient must be a non-nil value")
	}

	if config.KubeAPIResourceList == nil && config.KubeConfig == nil {
		return nil, fmt.Errorf("the configuration must have either KubeAPIResourceList or kubeConfig set")
	}

	return &TemplateResolver{
		// Required
		kubeClient: kubeClient,
		// Optional
		kubeAPIResourceList: config.KubeAPIResourceList,
		kubeConfig:          config.KubeConfig,
	}, nil
}

// HasTemplate performs a simple check for the template delimiter of "{{" to
// indicate if the input string has a template.
func HasTemplate(templateStr string, config Config) bool {
	glog.V(glogDefLvl).Infof("HasTemplate template str:  %v", templateStr)

	hasTemplate := false
	if strings.Contains(templateStr, "{{") {
		hasTemplate = true
	}

	glog.V(glogDefLvl).Infof("hasTemplate: %v", hasTemplate)

	return hasTemplate
}

// ResolveTemplate accepts an unmarshaled map that can be marshaled to YAML.
// It will process any template strings in it and return the processed map.
func (t *TemplateResolver) ResolveTemplate(tmplMap interface{}) (interface{}, error) {
	glog.V(glogDefLvl).Infof("ResolveTemplate for: %v", tmplMap)

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
	tmpl := template.New("tmpl").Funcs(funcMap)

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
	err = tmpl.Execute(&buf, "")
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
	re := regexp.MustCompile(`:\s+(?:[\|>][-]?\s+)?(?:['|"]\s*)?({{.*?\s+\|\s+(?:toInt|toBool)\s*}})(?:\s*['|"])?`)
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
