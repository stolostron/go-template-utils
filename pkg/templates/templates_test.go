// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stolostron/kubernetes-dependency-watches/client"
	yaml "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestNewResolver(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(&k8sClient, k8sConfig, Config{})
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

	testcases := []struct {
		kubeClient  *kubernetes.Interface
		config      Config
		expectedErr string
	}{
		{nil, Config{}, "kubeClient must be a non-nil value"},
		{
			&k8sClient,
			Config{StartDelim: "{{hub"},
			"the configurations StartDelim and StopDelim cannot be set independently",
		},
		{
			&k8sClient,
			Config{EncryptionConfig: EncryptionConfig{EncryptionEnabled: true}},
			"error validating EncryptionConfig: AESKey must be set to use this encryption mode",
		},
		{
			&k8sClient,
			Config{EncryptionConfig: EncryptionConfig{DecryptionEnabled: true}},
			"error validating EncryptionConfig: AESKey must be set to use this encryption mode",
		},
		{
			&k8sClient,
			Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: bytes.Repeat([]byte{byte('A')}, 256/8), EncryptionEnabled: true,
				},
			},
			"error validating EncryptionConfig: initialization vector must be set to use this encryption mode",
		},
	}

	for _, test := range testcases {
		test := test

		testName := fmt.Sprintf("expectedErr=%s", test.expectedErr)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			_, err := NewResolver(test.kubeClient, k8sConfig, test.config)
			if err == nil {
				t.Fatal("No error was provided")
			}

			if err.Error() != test.expectedErr {
				t.Fatalf("error \"%s\" != \"%s\"", err.Error(), test.expectedErr)
			}
		})
	}
}

type resolveTestCase struct {
	inputTmpl      string
	config         Config
	ctx            interface{}
	expectedResult string
	expectedErr    error
}

func doResolveTest(t *testing.T, test resolveTestCase) {
	t.Helper()

	tmplStr := []byte(test.inputTmpl)

	if !test.config.InputIsYAML {
		var err error
		tmplStr, err = yamlToJSON([]byte(test.inputTmpl))

		if err != nil {
			t.Fatalf(err.Error())
		}
	}

	resolver, err := NewResolver(&k8sClient, k8sConfig, test.config)
	if err != nil {
		t.Fatalf(err.Error())
	}

	tmplResult, err := resolver.ResolveTemplate(tmplStr, test.ctx)

	if err != nil {
		if test.expectedErr == nil {
			t.Fatalf(err.Error())
		}

		if !(errors.Is(err, test.expectedErr) || strings.EqualFold(test.expectedErr.Error(), err.Error())) {
			t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
		}
	} else {
		val, err := jsonToYAML(tmplResult.ResolvedJSON)
		if err != nil {
			t.Fatalf(err.Error())
		}
		valStr := strings.TrimSuffix(string(val), "\n")

		if valStr != test.expectedResult {
			t.Fatalf("%s expected : '%s' , got : '%s'", test.inputTmpl, test.expectedResult, val)
		}
	}
}

func TestResolveTemplateDefaultConfig(t *testing.T) {
	t.Parallel()

	testcases := map[string]resolveTestCase{
		"fromSecret": {
			inputTmpl:      `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			expectedResult: "data: c2VjcmV0a2V5MVZhbA==",
		},
		"fromConfigMap": {
			inputTmpl:      `param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			expectedResult: "param: cmkey1Val",
		},
		"toLiteral": {
			inputTmpl:      `param: '{{ fromConfigMap "testns" "testconfigmap" "ingressSources" | toLiteral }}'`,
			expectedResult: "param:\n  - 10.10.10.10\n  - 1.1.1.1",
		},
		"base64enc": {
			inputTmpl:      `config1: '{{ "testdata" | base64enc  }}'`,
			expectedResult: "config1: dGVzdGRhdGE=",
		},
		"base64dec": {
			inputTmpl:      `config2: '{{ "dGVzdGRhdGE=" | base64dec  }}'`,
			expectedResult: "config2: testdata",
		},
		"indent_pipe": {
			inputTmpl:      "spec:\n  config1: |\n    {{ " + `"hello\nworld\n"` + " | indent 4 }}\n",
			expectedResult: "spec:\n  config1: |\n    hello\n    world",
		},
		"indent_pipe_strip": {
			inputTmpl:      "spec:\n  config1: |-\n    {{ " + `"hello\nworld\n"` + " | indent 4 }}\n",
			expectedResult: "spec:\n  config1: hello world",
		},
		"autoindent_pipe": {
			inputTmpl:      "spec:\n  config1: |\n    {{ " + `"hello\nworld\n"` + " | autoindent }}\n",
			expectedResult: "spec:\n  config1: |\n    hello\n    world",
		},
		"autoindent_pipe_strip": {
			inputTmpl:      "spec:\n  config1: |-\n    {{ " + `"hello\nworld\n"` + " | autoindent }}\n",
			expectedResult: "spec:\n  config1: hello world",
		},
		"fromClusterClaim": {
			inputTmpl:      `value: '{{ fromClusterClaim "env" }}'`,
			expectedResult: "value: dev",
		},
	}

	for testName, test := range testcases {
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			doResolveTest(t, test)
		})
	}
}

func TestResolveTemplateErrors(t *testing.T) {
	t.Parallel()

	testcases := map[string]resolveTestCase{
		"toLiteral_with_newlines": {
			inputTmpl:   `param: '{{ "something\n  with\n  new\n lines\n" | toLiteral }}'`,
			expectedErr: ErrNewLinesNotAllowed,
		},
		"undefined_function": {
			inputTmpl: `test: '{{ blah "asdf"  }}'`,
			expectedErr: errors.New(
				`failed to parse the template JSON string {"test":"{{ blah \"asdf\"  }}"}: template: tmpl:1: ` +
					`function "blah" not defined`,
			),
		},
		"invalid_context_int": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         123,
			expectedErr: errors.New(`the input context must be a struct with string fields, got int`),
		},
		"invalid_context_nested_int": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         struct{ ClusterID int }{12},
			expectedErr: errors.New(`the input context must be a struct with string fields`),
		},
		"disabled_fromSecret": {
			inputTmpl: `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			config:    Config{DisabledFunctions: []string{"fromSecret"}},
			expectedErr: errors.New(
				`failed to parse the template JSON string {"data":"{{ fromSecret \"testns\" ` +
					`\"testsecret\" \"secretkey1\" }}"}: template: tmpl:1: function "fromSecret" ` +
					`not defined`,
			),
		},
		"missing_api_resource": {
			inputTmpl:   `value: '{{ lookup "v1" "NotAResource" "namespace" "object" }}'`,
			config:      Config{KubeAPIResourceList: []*metav1.APIResourceList{}},
			expectedErr: ErrMissingAPIResource,
		},
		"missing_api_resource_nested": {
			inputTmpl: `value: '{{ index (lookup "v1" "NotAResource" "namespace" "object").data.list 2 }}'`,
			config:    Config{KubeAPIResourceList: []*metav1.APIResourceList{}},
			expectedErr: errors.New(
				`one or more API resources are not installed on the API server which could have led to the ` +
					`templating error: template: tmpl:1:11: executing "tmpl" at <index (lookup "v1" "NotAResource" ` +
					`"namespace" "object").data.list 2>: error calling index: index of untyped nil: ` +
					`{"value":"{{ index (lookup \"v1\" \"NotAResource\" \"namespace\" \"object\").data.list 2 }}"}`,
			),
		},
	}

	for testName, test := range testcases {
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			doResolveTest(t, test)
		})
	}
}

func TestResolveTemplateWithConfig(t *testing.T) {
	t.Parallel()

	testcases := map[string]resolveTestCase{
		"ignores_default_delimiter": {
			inputTmpl:      `config1: '{{ "testdata" | base64enc  }}'`,
			config:         Config{StartDelim: "{{hub", StopDelim: "hub}}"},
			expectedResult: `config1: '{{ "testdata" | base64enc  }}'`,
		},
		"base64enc_hub": {
			inputTmpl:      `config1: '{{hub "testdata" | base64enc  hub}}'`,
			config:         Config{StartDelim: "{{hub", StopDelim: "hub}}"},
			expectedResult: "config1: dGVzdGRhdGE=",
		},
		"base64dec_hub_base64enc_regular": {
			inputTmpl:      `config1: '{{ "{{hub "dGVzdGRhdGE=" | base64dec hub}}" | base64enc }}'`,
			config:         Config{StartDelim: "{{hub", StopDelim: "hub}}"},
			expectedResult: `config1: '{{ "testdata" | base64enc }}'`,
		},
		"additionalIndentation": {
			inputTmpl:      "spec:\n  config1: |\n    {{hub " + `"hello\nworld\n"` + " | indent 2 hub}}\n",
			config:         Config{AdditionalIndentation: 2, StartDelim: "{{hub", StopDelim: "hub}}"},
			expectedResult: "spec:\n  config1: |\n    hello\n    world",
		},
		"additionalIndentation_autoindent": {
			inputTmpl:      "spec:\n  config1: |\n    {{ " + `"hello\nworld\n"` + " | autoindent }}\n",
			config:         Config{AdditionalIndentation: 4},
			expectedResult: "spec:\n  config1: |\n    hello\n    world",
		},
		"additionalIndentation_autoindent_again": {
			inputTmpl:      "spec:\n  autoindent-test: '{{ " + `"hello\nworld\nagain\n"` + " | autoindent }}'\n",
			config:         Config{AdditionalIndentation: 4},
			expectedResult: "spec:\n  autoindent-test: hello world again",
		},
		"inputIsYAML_fromSecret": {
			inputTmpl:      `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			config:         Config{InputIsYAML: true},
			expectedResult: "data: c2VjcmV0a2V5MVZhbA==",
		},
		"inputIsYAML_fromConfigMap": {
			inputTmpl:      `param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			config:         Config{InputIsYAML: true},
			expectedResult: "param: cmkey1Val",
		},
		"inputIsYAML_base64dec": {
			inputTmpl:      `config2: '{{ "dGVzdGRhdGE=" | base64dec  }}'`,
			config:         Config{InputIsYAML: true},
			expectedResult: "config2: testdata",
		},
	}

	for testName, test := range testcases {
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			doResolveTest(t, test)
		})
	}
}

func TestResolveTemplateWithContext(t *testing.T) {
	t.Parallel()

	testcases := map[string]resolveTestCase{
		"ClusterName": {
			inputTmpl:      `config1: '{{ .ClusterName  }}'`,
			ctx:            struct{ ClusterName string }{"cluster0001"},
			expectedResult: "config1: cluster0001",
		},
		"ClusterID_toInt": {
			inputTmpl:      `config1: '{{ .ClusterID | toInt }}'`,
			ctx:            struct{ ClusterID string }{"12345"},
			expectedResult: "config1: 12345",
		},
		"long_printf_base64": {
			inputTmpl: `test: '{{ printf "I am a really long template for cluster %s that needs to be over ` +
				`%d characters to test something" .ClusterName 80 | base64enc }}'`,
			ctx: struct{ ClusterName string }{"cluster1"},
			expectedResult: "test: SSBhbSBhIHJlYWxseSBsb25nIHRlbXBsYXRlIGZvciBjbHVzdGVyIGNsdXN0ZXIxIHRoYXQgbmVlZH" +
				"MgdG8gYmUgb3ZlciA4MCBjaGFyYWN0ZXJzIHRvIHRlc3Qgc29tZXRoaW5n",
		},
	}

	for testName, test := range testcases {
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			doResolveTest(t, test)
		})
	}
}

func TestResolveTemplateWithCrypto(t *testing.T) {
	t.Parallel()

	// Generate a 256 bit for AES-256. It can't be random so that the test results are deterministic.
	keyBytesSize := 256 / 8
	key := bytes.Repeat([]byte{byte('A')}, keyBytesSize)
	otherKey := bytes.Repeat([]byte{byte('B')}, keyBytesSize)
	iv := bytes.Repeat([]byte{byte('I')}, IVSize)

	encrypt := Config{
		EncryptionConfig: EncryptionConfig{
			AESKey:               key,
			EncryptionEnabled:    true,
			InitializationVector: iv,
		},
	}

	decrypt := Config{
		EncryptionConfig: EncryptionConfig{
			AESKey:               key,
			DecryptionEnabled:    true,
			InitializationVector: iv,
		},
	}

	testcases := map[string]resolveTestCase{
		"encrypt_protect": {
			inputTmpl:      `value: '{{ "Raleigh" | protect }}'`,
			config:         encrypt,
			expectedResult: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
		},
		"encrypt_protect_empty": {
			inputTmpl:      `value: '{{ "" | protect }}'`,
			config:         encrypt,
			expectedResult: `value: ""`,
		},
		"encrypt_fromSecret": {
			inputTmpl:      `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			config:         encrypt,
			expectedResult: "data: $ocm_encrypted:c6PNhsEfbM9NRUqeJ+HbcECCyVdFnRbLdd+n8r1fS9M=",
		},
		"decrypt": {
			inputTmpl:      "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			config:         decrypt,
			expectedResult: "value: Raleigh",
		},
		"decrypt_fallback_unused": {
			inputTmpl: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			config: Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, AESKeyFallback: otherKey, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: Raleigh",
		},
		"decrypt_fallback": {
			inputTmpl: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			config: Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: otherKey, AESKeyFallback: key, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: Raleigh",
		},
		"decryptionConcurrency": {
			inputTmpl: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==\n" +
				"value2: $ocm_encrypted:rBaGZbpT4WOXZzFI+XBrgg==\n" +
				"value3: $ocm_encrypted:rcKUPnLe4rejwXzsm2/g/w==",
			config: Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, DecryptionConcurrency: 5, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: Raleigh\nvalue2: Raleigh2\nvalue3: Raleigh3",
		},
		"nothing_to_decrypt": {
			inputTmpl:      "value: Raleigh",
			config:         decrypt,
			expectedResult: "value: Raleigh",
		},
		"decrypt_multiline": {
			inputTmpl:      "value: $ocm_encrypted:x7Ix9DQueY+gf08PM6VSVA==",
			config:         decrypt,
			expectedResult: "value: Hello\\nRaleigh",
		},
		"decrypt_ignores_nonbase64": {
			inputTmpl:      "value: $ocm_encrypted:😱😱😱😱",
			config:         decrypt,
			expectedResult: `value: "$ocm_encrypted:\U0001F631\U0001F631\U0001F631\U0001F631"`,
		},
		"decryption_disabled": {
			inputTmpl:      "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			config:         encrypt, // decryptionEnabled defaults to false
			expectedResult: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
		},
		"encrypt_and_decrypt": {
			inputTmpl: "value: '{{ \"Raleigh\" | protect }}'\nvalue2: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			config: Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, DecryptionEnabled: true, EncryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==\nvalue2: Raleigh",
		},
		"protect_not_enabled": {
			inputTmpl: `value: '{{ "Raleigh" | protect }}'`,
			config:    Config{EncryptionConfig: EncryptionConfig{AESKey: key, InitializationVector: iv}},
			expectedErr: errors.New(
				`failed to resolve the template {"value":"{{ \"Raleigh\" | protect }}"}: template: tmpl:1:23: ` +
					`executing "tmpl" at <protect>: error calling protect: the protect template function is not ` +
					`enabled in this mode`,
			),
		},
		"protect_not_enabled2": {
			inputTmpl: `value: '{{ "Raleigh" | protect }}'`,
			config:    decrypt,
			expectedErr: errors.New(
				`failed to resolve the template {"value":"{{ \"Raleigh\" | protect }}"}: template: tmpl:1:23: ` +
					`executing "tmpl" at <protect>: error calling protect: the protect template function is not ` +
					`enabled in this mode`,
			),
		},
		"encrypt_fails_illegalbase64": {
			inputTmpl: "value: $ocm_encrypted:==========",
			config:    decrypt,
			expectedErr: errors.New(
				"decryption of $ocm_encrypted:========== failed: ==========: the encrypted string is invalid " +
					"base64: illegal base64 data at input byte 0",
			),
		},
		"encrypt_fails_invalidpaddinglength": {
			inputTmpl: "value: $ocm_encrypted:mXIueuA3HvfBeobZZ0LdzA==",
			config:    decrypt,
			expectedErr: errors.New(
				`decryption of $ocm_encrypted:mXIueuA3HvfBeobZZ0LdzA== failed: invalid PCKS7 padding: the padding ` +
					`length is invalid`,
			),
		},
		"encrypt_fails_invalidpaddingbytes": {
			inputTmpl: "value: $ocm_encrypted:/X3LA2SczM7eqOLhZKAZXg==",
			config:    decrypt,
			expectedErr: errors.New(
				`decryption of $ocm_encrypted:/X3LA2SczM7eqOLhZKAZXg== failed: invalid PCKS7 padding: not all the ` +
					`padding bytes match`,
			),
		},
	}

	for testName, test := range testcases {
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			doResolveTest(t, test)
		})
	}
}

func TestReferencedObjects(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		inputTmpl       string
		errExpected     bool
		expectedRefObjs []client.ObjectIdentifier
	}{
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Secret",
					Namespace: testNs,
					Name:      "testsecret",
				},
			},
		},
		{
			`data: '{{ fromSecret "testns" "does-not-exist" "secretkey1" }}'`,
			true,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Secret",
					Namespace: testNs,
					Name:      "does-not-exist",
				},
			},
		},
		{
			`param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: testNs,
					Name:      "testconfigmap",
				},
			},
		},
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'` + "\n" +
				`param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Secret",
					Namespace: testNs,
					Name:      "testsecret",
				},
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: testNs,
					Name:      "testconfigmap",
				},
			},
		},
		{
			`config1: '{{ "testdata" | base64enc  }}'`,
			false,
			[]client.ObjectIdentifier{},
		},
		{
			`data: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'` + "\n" +
				`otherdata: '{{ fromConfigMap "testns" "does-not-exist" "cmkey23" }}'`,
			true,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: testNs,
					Name:      "testconfigmap",
				},
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: testNs,
					Name:      "does-not-exist",
				},
			},
		},
		{
			`value: '{{ fromClusterClaim "env" }}'`,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "cluster.open-cluster-management.io",
					Version:   "v1alpha1",
					Kind:      "ClusterClaim",
					Namespace: "",
					Name:      "env",
				},
			},
		},
		{
			`data: '{{ fromClusterClaim "does-not-exist" }}'`,
			true,
			[]client.ObjectIdentifier{
				{
					Group:     "cluster.open-cluster-management.io",
					Version:   "v1alpha1",
					Kind:      "ClusterClaim",
					Namespace: "",
					Name:      "does-not-exist",
				},
			},
		},
	}

	for _, test := range testcases {
		tmplStr, err := yamlToJSON([]byte(test.inputTmpl))
		if err != nil {
			t.Fatalf(err.Error())
		}

		resolver, err := NewResolver(&k8sClient, k8sConfig, Config{})
		if err != nil {
			t.Fatalf(err.Error())
		}

		tmplResult, err := resolver.ResolveTemplate(tmplStr, nil)
		if err != nil {
			if !test.errExpected {
				t.Fatalf(err.Error())
			}
		} else if test.errExpected {
			t.Fatalf("An error was expected but one was not received")
		}

		referencedObjs := tmplResult.ReferencedObjects

		if len(referencedObjs) != len(test.expectedRefObjs) ||
			((len(referencedObjs) != 0) && !reflect.DeepEqual(referencedObjs, test.expectedRefObjs)) {
			t.Errorf("got %q slice but expected %q", referencedObjs, test.expectedRefObjs)
		}
	}
}

func TestHasTemplate(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		input             string
		startDelim        string
		checkForEncrypted bool
		result            bool
	}{
		{" I am a sample template ", "{{", false, false},
		{" I am a sample template ", "", false, false},
		{" I am a {{ sample }}  template ", "{{", false, true},
		{`{"msg: "I am a {{ sample }} template"}`, "{{", false, true},
		{" I am a {{ sample }}  template ", "", false, true},
		{" I am a {{ sample }}  template ", "{{hub", false, false},
		{" I am a {{hub sample hub}}  template ", "{{hub", false, true},
		{" I am a $ocm_encrypted:abcdef template ", "", false, false},
		{" I am a $ocm_encrypted:abcdef template ", "", true, true},
	}

	for _, test := range testcases {
		val := HasTemplate([]byte(test.input), test.startDelim, test.checkForEncrypted)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestUsesEncryption(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		input      string
		startDelim string
		StopDelim  string
		result     bool
	}{
		{" I am a sample unencrypted template ", "{{", "}}", false},
		{" I am a sample unencrypted template ", "", "", false},
		{" I am a {{ sample }}  unencrypted template ", "{{", "}}", false},
		{" I am a {{ fromSecret test-secret }}  encrypted template ", "{{", "}}", true},
		{" I am a {{ test-secret | protect }}  encrypted template ", "{{", "}}", true},
		{`{"msg: "I am a {{ sample }} unencrypted template"}`, "{{", "}}", false},
		{`{"msg: "I am a {{ fromSecret test-secret }}  encrypted template"}`, "{{", "}}", true},
		{`{"msg: "I am a {{ test-secret | protect }}  encrypted template"}`, "{{", "}}", true},
		{" I am a {{ sample }}  unencrypted template ", "", "", false},
		{" I am a {{ fromSecret test-secret }}  encrypted template ", "", "", true},
		{" I am a {{ test-secret | protect }}  encrypted template ", "", "", true},
		{" I am a {{ sample }}  unencrypted template ", "{{hub", "hub}}", false},
		{" I am a {{ fromSecret test-secret }}  encrypted template ", "{{hub", "hub}}", false},
		{" I am a {{ test-secret | protect }}  encrypted template ", "{{hub", "hub}}", false},
		{" I am a {{hub sample hub}}  template ", "{{hub", "hub}}", false},
		{" I am a {{hub fromSecret test-secret hub}}  template ", "{{hub", "hub}}", true},
		{" I am a {{hub test-secret | protect hub}}  template ", "{{hub", "hub}}", true},
	}

	for _, test := range testcases {
		val := UsesEncryption([]byte(test.input), test.startDelim, test.StopDelim)
		if val != test.result {
			t.Fatalf("'%s' expected UsesEncryption : %v , got : %v", test.input, test.result, val)
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
		{
			`key : '{{ "1" | toBool }}'`,
			config,
			`key : {{ "1" | toBool }}`,
		},
		{
			`key : |
			'{{ "6" | toInt }}'`,
			config,
			`key : {{ "6" | toInt }}`,
		},
		{
			`key1 : '{{ "1" | toInt }}'
		     key2 : |-
		 		{{ "test" | toBool | toInt }}`,
			config,
			`key1 : {{ "1" | toInt }}
		     key2 : {{ "test" | toBool | toInt }}`,
		},
		{
			`key : '{{hub "1" | toBool hub}}'`,
			hubConfig,
			`key : {{hub "1" | toBool hub}}`,
		},
		{
			`key : '{{ if fromClusterClaim "something"}} 1 {{ else }} {{ 2 | toInt }} {{ end }} }}'`,
			config,
			`key : {{ if fromClusterClaim "something"}} 1 {{ else }} {{ 2 | toInt }} {{ end }} }}`,
		},
		{
			`key1 : '{{ "something"}} {{ "false" | toBool }} {{ else }} {{ "true" | toBool }} {{ end }} }}'
		     key2 : '{{ "blah" | print }}'`,
			config,
			`key1 : {{ "something"}} {{ "false" | toBool }} {{ else }} {{ "true" | toBool }} {{ end }} }}
		     key2 : '{{ "blah" | print }}'`,
		},
		{
			`key1 : 'testval1'
		     key2 : '{{with fromConfigMap "namespace" "name" "key" }} {{ . | toInt }} {{ else }} 2 {{ end }} }}'
		     key3 : '{{ "blah" | toBool }}'`,
			config,
			`key1 : 'testval1'
		     key2 : {{with fromConfigMap "namespace" "name" "key" }} {{ . | toInt }} {{ else }} 2 {{ end }} }}
		     key3 : {{ "blah" | toBool }}`,
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(&k8sClient, k8sConfig, test.config)
		if err != nil {
			t.Fatalf(err.Error())
		}

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
		config := Config{LookupNamespace: test.configuredNamespace}
		resolver, _ := NewResolver(&k8sClient, k8sConfig, config)

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

// nolint: nosnakecase
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

	resolver, err := NewResolver(&k8sClient, k8sConfig, Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to instantiate the templatesResolver struct: %v\n", err)
		panic(err)
	}

	templateContext := struct{ ClusterName string }{ClusterName: "cluster0001"}

	tmplResult, err := resolver.ResolveTemplate(policyJSON, templateContext)
	policyResolvedJSON := tmplResult.ResolvedJSON

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

func TestSetEncryptionConfig(t *testing.T) {
	t.Parallel()
	// Generate a 256 bit for AES-256. It can't be random so that the test results are deterministic.
	keyBytesSize := 256 / 8
	key := bytes.Repeat([]byte{byte('A')}, keyBytesSize)
	otherKey := bytes.Repeat([]byte{byte('B')}, keyBytesSize)
	iv := bytes.Repeat([]byte{byte('I')}, IVSize)
	otherIV := bytes.Repeat([]byte{byte('I')}, IVSize)

	tests := []struct {
		encryptionConfig EncryptionConfig
		expectedError    error
	}{
		{
			EncryptionConfig{
				EncryptionEnabled:    true,
				AESKey:               key,
				InitializationVector: iv,
			}, nil,
		},
		{
			EncryptionConfig{
				DecryptionEnabled:    true,
				AESKey:               otherKey,
				InitializationVector: otherIV,
			}, nil,
		},
		{
			EncryptionConfig{
				EncryptionEnabled: false,
				DecryptionEnabled: false,
			}, nil,
		},
		{
			EncryptionConfig{
				EncryptionEnabled: true,
				AESKey:            []byte{byte('A')},
			}, fmt.Errorf("%w: %s", ErrInvalidAESKey, "crypto/aes: invalid key size 1"),
		},
		{
			EncryptionConfig{
				EncryptionEnabled: true,
				AESKey:            key,
				AESKeyFallback:    []byte{byte('A')},
			}, fmt.Errorf("%w: %s", ErrInvalidAESKey, "crypto/aes: invalid key size 1"),
		},
		{
			EncryptionConfig{
				EncryptionEnabled: true,
				AESKey:            key,
			}, ErrIVNotSet,
		},
		{
			EncryptionConfig{
				EncryptionEnabled:    true,
				InitializationVector: []byte{byte('A')},
			}, ErrAESKeyNotSet,
		},
		{
			EncryptionConfig{
				DecryptionEnabled:    true,
				AESKey:               otherKey,
				InitializationVector: []byte{byte('A')},
			}, ErrInvalidIV,
		},
	}

	config := Config{}
	resolver, _ := NewResolver(&k8sClient, k8sConfig, config)

	for _, test := range tests {
		err := resolver.SetEncryptionConfig(test.encryptionConfig)

		if err == nil || test.expectedError == nil {
			if !(err == nil && test.expectedError == nil) {
				t.Fatalf("expected error: %v, got: %v", test.expectedError, err)
			}
		} else if err.Error() != test.expectedError.Error() {
			t.Fatalf("expected error: %v, got: %v", test.expectedError, err)
		}
	}
}

func TestSetKubeAPIResourceList(t *testing.T) {
	resolver := TemplateResolver{}

	if len(resolver.config.KubeAPIResourceList) != 0 {
		t.Fatalf("expected the initial value of config.KubeAPIResourceList to be empty")
	}

	apiResourceList := []*metav1.APIResourceList{{}}
	resolver.SetKubeAPIResourceList(apiResourceList)

	if len(resolver.config.KubeAPIResourceList) != 1 {
		t.Fatalf("expected the set value of config.KubeAPIResourceList to have one entry")
	}
}
