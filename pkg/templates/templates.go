// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
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

var (
	kubeClient          *kubernetes.Interface
	kubeConfig          *rest.Config
	kubeAPIResourceList []*metav1.APIResourceList
)

func InitializeKubeClient(k8sClient *kubernetes.Interface, k8sConfig *rest.Config) {
	kubeClient = k8sClient
	kubeConfig = k8sConfig
}

// If this is set, template processing will not try to rediscover
// the apiresourcesList needed for dynamic client/ gvk look.
func SetAPIResources(apiresList []*metav1.APIResourceList) {
	kubeAPIResourceList = apiresList
}

// HasTemplate does a simple check for a "{{" string to indicate if it has a template.
func HasTemplate(templateStr string) bool {
	glog.V(glogDefLvl).Infof("hasTemplate template str:  %v", templateStr)

	hasTemplate := false
	if strings.Contains(templateStr, "{{") {
		hasTemplate = true
	}

	glog.V(glogDefLvl).Infof("hasTemplate: %v", hasTemplate)

	return hasTemplate
}

// Main Template Processing func.
func ResolveTemplate(tmplMap interface{}) (interface{}, error) {
	glog.V(glogDefLvl).Infof("ResolveTemplate for: %v", tmplMap)

	// Build Map of supported template functions
	funcMap := template.FuncMap{
		"fromSecret":       fromSecret,
		"fromConfigMap":    fromConfigMap,
		"fromClusterClaim": fromClusterClaim,
		"lookup":           lookup,
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
		templateStr = processForDataTypes(templateStr)
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

func processForDataTypes(str string) string {
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
