// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stolostron/kubernetes-dependency-watches/client"
	yaml "gopkg.in/yaml.v3"
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
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
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
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testconfigmap",
		},
		Data: map[string]string{
			"cmkey1":         "cmkey1Val",
			"cmkey2":         "cmkey2Val",
			"ingressSources": "[10.10.10.10, 1.1.1.1]",
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
			Config{EncryptionConfig: EncryptionConfig{EncryptionEnabled: true}},
			"error validating EncryptionConfig: AESKey must be set to use this encryption mode",
		},
		{
			&simpleClient,
			Config{EncryptionConfig: EncryptionConfig{DecryptionEnabled: true}},
			"error validating EncryptionConfig: AESKey must be set to use this encryption mode",
		},
		{
			&simpleClient,
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
	otherKey := bytes.Repeat([]byte{byte('B')}, keyBytesSize)
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
			`param: '{{ fromConfigMap "testns" "testconfigmap" "ingressSources" | toLiteral }}'`,
			Config{},
			nil,
			"param:\n  - 10.10.10.10\n  - 1.1.1.1",
			nil,
		},
		{
			`param: '{{ "something\n  with\n  new\n lines\n" | toLiteral }}'`,
			Config{},
			nil,
			"",
			ErrNewLinesNotAllowed,
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
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, EncryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			nil,
		},
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, EncryptionEnabled: true, InitializationVector: iv}},
			nil,
			"data: $ocm_encrypted:c6PNhsEfbM9NRUqeJ+HbcECCyVdFnRbLdd+n8r1fS9M=",
			nil,
		},
		{
			`value: '{{ "" | protect }}'`,
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, EncryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			`value: ""`,
			nil,
		},
		{
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"value: Raleigh",
			nil,
		},
		{
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, AESKeyFallback: otherKey, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			struct{}{},
			"value: Raleigh",
			nil,
		},
		{
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: otherKey, AESKeyFallback: key, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			struct{}{},
			"value: Raleigh",
			nil,
		},
		{
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==\n" +
				"value2: $ocm_encrypted:rBaGZbpT4WOXZzFI+XBrgg==\n" +
				"value3: $ocm_encrypted:rcKUPnLe4rejwXzsm2/g/w==",
			Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, DecryptionConcurrency: 5, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			struct{}{},
			"value: Raleigh\nvalue2: Raleigh2\nvalue3: Raleigh3",
			nil,
		},
		{
			"value: Raleigh", // No encryption string to decrypt
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"value: Raleigh",
			nil,
		},
		{
			"value: $ocm_encrypted:x7Ix9DQueY+gf08PM6VSVA==", // Encrypted multiline string
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"value: Hello\\nRaleigh",
			nil,
		},
		{
			"value: $ocm_encrypted:😱😱😱😱", // Not Base64
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			`value: "$ocm_encrypted:\U0001F631\U0001F631\U0001F631\U0001F631"`,
			nil,
		},
		{
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==", // Will not be decrypted because of the encryption mode
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, EncryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			nil,
		},
		{
			// Both encryption and decryption are enabled
			"value: '{{ \"Raleigh\" | protect }}'\nvalue2: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			Config{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, DecryptionEnabled: true, EncryptionEnabled: true, InitializationVector: iv,
				},
			},
			struct{}{},
			"value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==\nvalue2: Raleigh",
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
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, InitializationVector: iv}},
			struct{}{},
			"",
			errors.New(
				`failed to resolve the template {"value":"{{ \"Raleigh\" | protect }}"}: template: tmpl:1:23: ` +
					`executing "tmpl" at <protect>: error calling protect: the protect template function is not ` +
					`enabled in this mode`,
			),
		},
		{
			`value: '{{ "Raleigh" | protect }}'`,
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"",
			errors.New(
				`failed to resolve the template {"value":"{{ \"Raleigh\" | protect }}"}: template: tmpl:1:23: ` +
					`executing "tmpl" at <protect>: error calling protect: the protect template function is not ` +
					`enabled in this mode`,
			),
		},
		{
			"value: $ocm_encrypted:==========",
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"",
			errors.New(
				"decryption of $ocm_encrypted:========== failed: ==========: the encrypted string is invalid " +
					"base64: illegal base64 data at input byte 0",
			),
		},
		{
			"value: $ocm_encrypted:mXIueuA3HvfBeobZZ0LdzA==",
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"",
			errors.New(
				`decryption of $ocm_encrypted:mXIueuA3HvfBeobZZ0LdzA== failed: invalid PCKS7 padding: the padding ` +
					`length is invalid`,
			),
		},
		{
			"value: $ocm_encrypted:/X3LA2SczM7eqOLhZKAZXg==",
			Config{EncryptionConfig: EncryptionConfig{AESKey: key, DecryptionEnabled: true, InitializationVector: iv}},
			struct{}{},
			"",
			errors.New(
				`decryption of $ocm_encrypted:/X3LA2SczM7eqOLhZKAZXg== failed: invalid PCKS7 padding: not all the ` +
					`padding bytes match`,
			),
		},
		{
			`value: '{{ lookup "v1" "NotAResource" "namespace" "object" }}'`,
			Config{KubeAPIResourceList: []*metav1.APIResourceList{}},
			struct{}{},
			"",
			ErrMissingAPIResource,
		},
		{
			`value: '{{ index (lookup "v1" "NotAResource" "namespace" "object").data.list 2 }}'`,
			Config{KubeAPIResourceList: []*metav1.APIResourceList{}},
			struct{}{},
			"",
			errors.New(
				`one or more API resources are not installed on the API server which could have led to the ` +
					`templating error: template: tmpl:1:11: executing "tmpl" at <index (lookup "v1" "NotAResource" ` +
					`"namespace" "object").data.list 2>: error calling index: index of untyped nil: ` +
					`{"value":"{{ index (lookup \"v1\" \"NotAResource\" \"namespace\" \"object\").data.list 2 }}"}`,
			),
		},
	}

	for _, test := range testcases {
		tmplStr, err := yamlToJSON([]byte(test.inputTmpl))
		if err != nil {
			t.Fatalf(err.Error())
		}

		resolver := getTemplateResolver(test.config)
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
}

func TestReferencedObjects(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		inputTmpl       string
		config          Config
		ctx             interface{}
		errExpected     bool
		expectedRefObjs []client.ObjectIdentifier
	}{
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			Config{},
			nil,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Secret",
					Namespace: "testns",
					Name:      "testsecret",
				},
			},
		},
		{
			`param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			Config{},
			nil,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: "testns",
					Name:      "testconfigmap",
				},
			},
		},
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'` + "\n" +
				`param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			Config{},
			nil,
			false,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Secret",
					Namespace: "testns",
					Name:      "testsecret",
				},
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: "testns",
					Name:      "testconfigmap",
				},
			},
		},
		{
			`config1: '{{ "testdata" | base64enc  }}'`,
			Config{},
			nil,
			false,
			[]client.ObjectIdentifier{},
		},
		{
			`data: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'` + "\n" +
				`otherdata: '{{ fromConfigMap "testns" "does-not-exist" "cmkey23" }}'`,
			Config{},
			nil,
			true,
			[]client.ObjectIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "ConfigMap",
					Namespace: "testns",
					Name:      "testconfigmap",
				},
			},
		},
	}

	for _, test := range testcases {
		tmplStr, err := yamlToJSON([]byte(test.inputTmpl))
		if err != nil {
			t.Fatalf(err.Error())
		}

		resolver := getTemplateResolver(test.config)

		tmplResult, err := resolver.ResolveTemplate(tmplStr, test.ctx)
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

	// This example uses the fake Kubernetes client, but in production, use a
	// real Kubernetes configuration and client
	var k8sClient kubernetes.Interface = fake.NewSimpleClientset()

	resolver, err := NewResolver(&k8sClient, &rest.Config{}, Config{})
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

	var simpleClient kubernetes.Interface = fake.NewSimpleClientset()

	config := Config{}
	resolver, _ := NewResolver(&simpleClient, &rest.Config{}, config)

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
