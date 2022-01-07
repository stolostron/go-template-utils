// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
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

	_, err := simpleClient.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

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

	_, err = simpleClient.CoreV1().Secrets(testns).Create(context.TODO(), &secret, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

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

	_, err = simpleClient.CoreV1().ConfigMaps(testns).Create(context.TODO(), &configmap, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

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

	if resolver.config.StartDelim != "{{" || resolver.config.StopDelim != "}}" {
		t.Fatalf(
			"Expected delimiters: {{ and }}  got: %s and %s",
			resolver.config.StartDelim,
			resolver.config.StopDelim,
		)
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
		{
			&simpleClient,
			Config{StartDelim: "{{hub"},
			"the configurations StartDelim and StopDelim cannot be set independently",
		},
		{
			&simpleClient,
			Config{EncryptionMode: EncryptionEnabled},
			"AESKey must be set to use this encryption mode",
		},
		{
			&simpleClient,
			Config{EncryptionMode: DecryptionEnabled},
			"AESKey must be set to use this encryption mode",
		},
		{
			&simpleClient,
			Config{EncryptionMode: EncryptionEnabled, AESKey: bytes.Repeat([]byte{byte('A')}, 256/8)},
			"InitializationVector must be 128 bits",
		},
	}

	for _, test := range testcases {
		test := test

		testName := fmt.Sprintf("expectedErr=%s", test.expectedErr)
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

	// Generate a 256 bit for AES-256. It can't be random so that the test results are deterministic.
	keyBytesSize := 256 / 8
	key := bytes.Repeat([]byte{byte('A')}, keyBytesSize)
	iv := bytes.Repeat([]byte{byte('I')}, IVSize)

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
			errors.New(
				`failed to parse the template JSON string {"test":"{{ blah \"asdf\"  }}"}: template: tmpl:1: ` +
					`function "blah" not defined`,
			),
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
			"spec:\n  config1: |\n    {{ " + `"hello\nworld\n"` + " | indent 4 }}\n",
			Config{},
			struct{}{},
			"spec:\n  config1: |\n    hello\n    world",
			nil,
		},
		{
			"spec:\n  config1: |-\n    {{ " + `"hello\nworld\n"` + " | indent 4 }}\n",
			Config{},
			struct{}{},
			"spec:\n  config1: hello world",
			nil,
		},
		{
			"spec:\n  config1: |\n    {{hub " + `"hello\nworld\n"` + " | indent 2 hub}}\n",
			Config{AdditionalIndentation: 2, StartDelim: "{{hub", StopDelim: "hub}}"},
			struct{}{},
			"spec:\n  config1: |\n    hello\n    world",
			nil,
		},
		{
			"spec:\n  config1: |\n    {{ " + `"hello\nworld\n"` + " | autoindent }}\n",
			Config{},
			struct{}{},
			"spec:\n  config1: |\n    hello\n    world",
			nil,
		},
		{
			"spec:\n  config1: |-\n    {{ " + `"hello\nworld\n"` + " | autoindent }}\n",
			Config{},
			struct{}{},
			"spec:\n  config1: hello world",
			nil,
		},
		{
			"spec:\n  config1: |\n    {{ " + `"hello\nworld\n"` + " | autoindent }}\n",
			Config{AdditionalIndentation: 4},
			struct{}{},
			"spec:\n  config1: |\n    hello\n    world",
			nil,
		},
		{
			"spec:\n  autoindent-test: '{{ " + `"hello\nworld\nagain\n"` + " | autoindent }}'\n",
			Config{AdditionalIndentation: 4},
			struct{}{},
			"spec:\n  autoindent-test: hello world again",
			nil,
		},
		{
			`value: '{{ "Raleigh" | protect }}'`,
			Config{AESKey: key, EncryptionMode: EncryptionEnabled, InitializationVector: iv},
			struct{}{},
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			nil,
		},
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			Config{AESKey: key, EncryptionMode: EncryptionEnabled, InitializationVector: iv},
			nil,
			"data: $ocm_encrypted:c6PNhsEfbM9NRUqeJ+HbcECCyVdFnRbLdd+n8r1fS9M=",
			nil,
		},
		{
			`value: '{{ "" | protect }}'`,
			Config{AESKey: key, EncryptionMode: EncryptionEnabled, InitializationVector: iv},
			struct{}{},
			`value: ""`,
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
		{
			`test: '{{ printf "I am a really long template for cluster %s that needs to be over ` +
				`%d characters to test something" .ClusterName 80 | base64enc }}'`,
			Config{},
			struct{ ClusterName string }{"cluster1"},
			"test: SSBhbSBhIHJlYWxseSBsb25nIHRlbXBsYXRlIGZvciBjbHVzdGVyIGNsdXN0ZXIxIHRoYXQgbmVlZH" +
				"MgdG8gYmUgb3ZlciA4MCBjaGFyYWN0ZXJzIHRvIHRlc3Qgc29tZXRoaW5n",
			nil,
		},
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			Config{DisabledFunctions: []string{"fromSecret"}},
			nil,
			"",
			errors.New(
				`failed to parse the template JSON string {"data":"{{ fromSecret \"testns\" ` +
					`\"testsecret\" \"secretkey1\" }}"}: template: tmpl:1: function "fromSecret" ` +
					`not defined`,
			),
		},
		{
			`value: '{{ "Raleigh" | protect }}'`,
			Config{AESKey: []byte{byte('A')}, EncryptionMode: EncryptionEnabled, InitializationVector: iv},
			nil,
			"",
			errors.New(
				`failed to resolve the template {"value":"{{ \"Raleigh\" | protect }}"}: template: tmpl:1:23: ` +
					`executing "tmpl" at <protect>: error calling protect: the AES key is invalid: crypto/aes: ` +
					`invalid key size 1`,
			),
		},
	}

	for _, test := range testcases {
		tmplStr, _ := yamlToJSON([]byte(test.inputTmpl))
		resolver := getTemplateResolver(test.config)

		val, err := resolver.ResolveTemplate(tmplStr, test.ctx)
		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}

			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else {
			val, _ := jsonToYAML(val)
			valStr := strings.TrimSuffix(string(val), "\n")

			if valStr != test.expectedResult {
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
		{`{"msg: "I am a {{ sample }} template"}`, "{{", true},
		{" I am a {{ sample }}  template ", "", true},
		{" I am a {{ sample }}  template ", "{{hub", false},
		{" I am a {{hub sample hub}}  template ", "{{hub", true},
	}

	for _, test := range testcases {
		val := HasTemplate([]byte(test.input), test.startDelim)
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

func TestGetNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		funcName            string
		configuredNamespace string
		actualNamespace     string
		returnedNamespace   string
		expectedError       error
	}{
		{"fromConfigMap", "my-policies", "my-policies", "my-policies", nil},
		{"fromConfigMap", "", "prod-configs", "prod-configs", nil},
		{"fromConfigMap", "my-policies", "", "my-policies", nil},
		{
			"fromConfigMap",
			"my-policies",
			"prod-configs",
			"",
			errors.New("the namespace argument passed to fromConfigMap is restricted to my-policies"),
		},
		{
			"fromConfigMap",
			"policies",
			"prod-configs",
			"",
			errors.New("the namespace argument passed to fromConfigMap is restricted to policies"),
		},
	}

	for _, test := range tests {
		var simpleClient kubernetes.Interface = fake.NewSimpleClientset()

		config := Config{LookupNamespace: test.configuredNamespace}
		resolver, _ := NewResolver(&simpleClient, &rest.Config{}, config)

		ns, err := resolver.getNamespace(test.funcName, test.actualNamespace)

		if err == nil || test.expectedError == nil {
			if !(err == nil && test.expectedError == nil) {
				t.Fatalf("expected error: %v, got: %v", test.expectedError, err)
			}

			if ns != test.returnedNamespace {
				t.Fatalf("expected namespace: %s, got: %s", test.actualNamespace, ns)
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
  remediationAction: enforce
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
    severity: high
`

	policyJSON, err := yamlToJSON([]byte(policyYAML))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to convert the policy YAML to JSON: %v\n", err)
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

	policyResolvedJSON, err := resolver.ResolveTemplate(policyJSON, templateContext)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	var policyResolved interface{}
	err = yaml.Unmarshal(policyResolvedJSON, &policyResolved)

	objTmpls := policyResolved.(map[string]interface{})["spec"].(map[string]interface{})["object-templates"]
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
