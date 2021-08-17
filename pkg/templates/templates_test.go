// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

func getTemplateResolver(c Config) *TemplateResolver {
	var simpleClient kubernetes.Interface = fake.NewSimpleClientset()

	// setup test resources

	testns := "testns"
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testns,
		},
	}
	simpleClient.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})

	// secret
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testsecret",
		},
		Data: map[string][]byte{
			"secretkey1": []byte("secretkey1Val"),
			"secretkey2": []byte("secretkey2Val"),
		},
		Type: "Opaque",
	}
	simpleClient.CoreV1().Secrets(testns).Create(context.TODO(), &secret, metav1.CreateOptions{})

	// configmap
	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testconfigmap",
		},
		Data: map[string]string{
			"cmkey1": "cmkey1Val",
			"cmkey2": "cmkey2Val",
		},
	}
	simpleClient.CoreV1().ConfigMaps(testns).Create(context.TODO(), &configmap, metav1.CreateOptions{})

	resolver, err := NewResolver(&simpleClient, &rest.Config{}, c)
	if err != nil {
		panic(err.Error())
	}

	return resolver
}

func TestNewResolver(t *testing.T) {
	t.Parallel()
	var simpleClient kubernetes.Interface = fake.NewSimpleClientset()

	resolver, err := NewResolver(&simpleClient, &rest.Config{}, Config{})
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	if resolver.startDelim != "{{" || resolver.stopDelim != "}}" {
		t.Fatalf("Expected delimiters: {{ and }}  got: %s and %s", resolver.startDelim, resolver.stopDelim)
	}
}

func TestNewResolverFailures(t *testing.T) {
	t.Parallel()
	var simpleClient kubernetes.Interface = fake.NewSimpleClientset()

	testcases := []struct {
		kubeClient  *kubernetes.Interface
		config      Config
		expectedErr string
	}{
		{nil, Config{}, "kubeClient must be a non-nil value"},
		{&simpleClient, Config{StartDelim: "{{hub"}, "the configurations StartDelim and StopDelim cannot be set independently"},
	}

	for _, test := range testcases {
		testName := fmt.Sprintf("expectedErr=%s", test.expectedErr)
		test := test
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			_, err := NewResolver(test.kubeClient, &rest.Config{}, test.config)
			if err == nil {
				t.Fatal("No error was provided")
			}

			if err.Error() != test.expectedErr {
				t.Fatalf("error \"%s\" != \"%s\"", err.Error(), test.expectedErr)
			}
		})
	}
}

func TestResolveTemplate(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		inputTmpl      string
		config         Config
		ctx            interface{}
		expectedResult string
		expectedErr    error
	}{
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			Config{},
			nil,
			"data: c2VjcmV0a2V5MVZhbA==",
			nil,
		},
		{
			`param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			Config{},
			nil,
			"param: cmkey1Val",
			nil,
		},
		{
			`config1: '{{ "testdata" | base64enc  }}'`,
			Config{},
			nil,
			"config1: dGVzdGRhdGE=",
			nil,
		},
		{
			`config2: '{{ "dGVzdGRhdGE=" | base64dec  }}'`,
			Config{},
			nil,
			"config2: testdata",
			nil,
		},
		{
			`test: '{{ blah "asdf"  }}'`,
			Config{},
			nil,
			"",
			errors.New(`failed to parse the template map map[test:{{ blah "asdf"  }}]: template: tmpl:1: function "blah" not defined`),
		},
		{
			`config1: '{{ "testdata" | base64enc  }}'`,
			Config{StartDelim: "{{hub", StopDelim: "hub}}"},
			nil,
			`config1: '{{ "testdata" | base64enc  }}'`,
			nil,
		},
		{
			`config1: '{{hub "testdata" | base64enc  hub}}'`,
			Config{StartDelim: "{{hub", StopDelim: "hub}}"},
			nil,
			"config1: dGVzdGRhdGE=",
			nil,
		},
		{
			`config1: '{{ "{{hub "dGVzdGRhdGE=" | base64dec hub}}" | base64enc }}'`,
			Config{StartDelim: "{{hub", StopDelim: "hub}}"},
			nil,
			`config1: '{{ "testdata" | base64enc }}'`,
			nil,
		},
		{
			`config1: '{{ .ClusterName  }}'`,
			Config{},
			struct{ ClusterName string }{"cluster0001"},
			"config1: cluster0001",
			nil,
		},
		{
			`config1: '{{ .ClusterID | toInt }}'`,
			Config{},
			struct{ ClusterID string }{"12345"},
			"config1: 12345",
			nil,
		},
		{
			`test: '{{ printf "hello %s" "world" }}'`,
			Config{},
			123,
			"",
			errors.New(`the input context must be a struct with string fields, got int`),
		},
		{
			`test: '{{ printf "hello %s" "world" }}'`,
			Config{},
			struct{ ClusterID int }{12},
			"",
			errors.New(`the input context must be a struct with string fields`),
		},
	}

	for _, test := range testcases {
		// unmarshall to Interface
		tmplMap, _ := fromYAML(test.inputTmpl)
		resolver := getTemplateResolver(test.config)
		val, err := resolver.ResolveTemplate(tmplMap, test.ctx)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}
			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else {
			val, _ := toYAML(val)
			if val != test.expectedResult {
				t.Fatalf("expected : %s , got : %s", test.expectedResult, val)
			}
		}
	}
}

func TestHasTemplate(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input      string
		startDelim string
		result     bool
	}{
		{" I am a sample template ", "{{", false},
		{" I am a sample template ", "", false},
		{" I am a {{ sample }}  template ", "{{", true},
		{" I am a {{ sample }}  template ", "", true},
		{" I am a {{ sample }}  template ", "{{hub", false},
		{" I am a {{hub sample hub}}  template ", "{{hub", true},
	}

	for _, test := range testcases {
		val := HasTemplate(test.input, test.startDelim)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestAtoi(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input  string
		result int
	}{
		{"123", 123},
	}

	for _, test := range testcases {
		val := atoi(test.input)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestToBool(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input  string
		result bool
	}{
		{"1", true},
		{"blah", false},
		{"TRUE", true},
		{"F", false},
		{"true", true},
		{"false", false},
	}

	for _, test := range testcases {
		val := toBool(test.input)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestProcessForDataTypes(t *testing.T) {
	t.Parallel()
	config := Config{StartDelim: "{{", StopDelim: "}}"}
	hubConfig := Config{StartDelim: "{{hub", StopDelim: "hub}}"}
	testcases := []struct {
		input          string
		config         Config
		expectedResult string
	}{
		{`key : "{{ "1" | toBool }}"`, config, `key : {{ "1" | toBool }}`},
		{
			`key : |
			"{{ "6" | toInt }}"`,
			config,
			`key : {{ "6" | toInt }}`,
		},
		{
			`key1 : "{{ "1" | toInt }}"
		  key2 : |-
		 		{{ "test" | toBool | toInt }}`,
			config,
			`key1 : {{ "1" | toInt }}
		  key2 : {{ "test" | toBool | toInt }}`,
		},
		{`key : "{{hub "1" | toBool hub}}"`, hubConfig, `key : {{hub "1" | toBool hub}}`},
	}

	for _, test := range testcases {
		resolver := getTemplateResolver(test.config)
		val := resolver.processForDataTypes(test.input)
		if val != test.expectedResult {
			t.Fatalf("expected : %v , got : %v", test.expectedResult, val)
		}
	}
}

func TestVerifyNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		funcName            string
		configuredNamespace string
		actualNamespace     string
		expectedError       error
	}{
		{"fromConfigMap", "my-policies", "my-policies", nil},
		{"fromConfigMap", "", "prod-configs", nil},
		{"fromConfigMap", "my-policies", "prod-configs", errors.New("the namespace argument passed to fromConfigMap is restricted to my-policies")},
		{"fromConfigMap", "policies", "prod-configs", errors.New("the namespace argument passed to fromConfigMap is restricted to policies")},
	}

	for _, test := range tests {
		var simpleClient kubernetes.Interface = fake.NewSimpleClientset()
		config := Config{LookupNamespace: test.configuredNamespace}
		resolver, _ := NewResolver(&simpleClient, &rest.Config{}, config)

		err := resolver.verifyNamespace(test.funcName, test.actualNamespace)

		if err == nil || test.expectedError == nil {
			if !(err == nil && test.expectedError == nil) {
				t.Fatalf("expected error: %v, got: %v", test.expectedError, err)
			}
		} else if err.Error() != test.expectedError.Error() {
			t.Fatalf("expected error: %v, got: %v", test.expectedError, err)
		}
	}
}

func ExampleTemplateResolver_ResolveTemplate() {
	policyYAML := `
---
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: demo-sampleapp-config
  namespace: sampleapp
spec:
  namespaceSelector:
    exclude:
    - kube-*
    include:
    - default
  object-templates:
  - complianceType: musthave
    objectDefinition:
      kind: ConfigMap
      apiVersion: v1
      metadata:
        name: demo-sampleapp-config
        namespace: test
      data:
        message: '{{ "VGVtcGxhdGVzIHJvY2sh" | base64dec }}'
        b64-cluster-name: '{{ .ClusterName | base64enc }}'
    remediationAction: enforce
    severity: high
`
	policyMap := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(policyYAML), &policyMap); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal the policy YAML: %v\n", err)
		panic(err)
	}

	// This example uses the fake Kubernetes client, but in production, use a
	// real Kubernetes configuration and client
	var k8sClient kubernetes.Interface = fake.NewSimpleClientset()
	resolver, err := NewResolver(&k8sClient, &rest.Config{}, Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to instantiate the templatesResolver struct: %v\n", err)
		panic(err)
	}

	templateContext := struct{ ClusterName string }{ClusterName: "cluster0001"}
	policyMapProcessed, err := resolver.ResolveTemplate(policyMap, templateContext)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	objTmpls := policyMapProcessed.(map[string]interface{})["spec"].(map[string]interface{})["object-templates"]
	objDef := objTmpls.([]interface{})[0].(map[string]interface{})["objectDefinition"]
	data, ok := objDef.(map[string]interface{})["data"].(map[string]interface{})
	if !ok {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	message, ok := data["message"].(string)
	if !ok {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	b64ClusterName, ok := data["b64-cluster-name"].(string)
	if !ok {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	fmt.Println(message)
	fmt.Println(b64ClusterName)

	// Output:
	// Templates rock!
	// Y2x1c3RlcjAwMDE=
}
