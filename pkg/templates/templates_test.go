// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stolostron/kubernetes-dependency-watches/client"
	yaml "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNewResolver(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(k8sConfig, Config{})
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

func TestNewResolverWithCaching(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	resolver, _, err := NewResolverWithCaching(ctx, k8sConfig, Config{})
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

func TestNewResolverWithDynamicWatcher(t *testing.T) {
	t.Parallel()

	dynWatcher, err := client.New(k8sConfig, fakeReconciler{}, &client.Options{EnableCache: true})
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	resolver, err := NewResolverWithDynamicWatcher(dynWatcher, Config{SkipBatchManagement: true})
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
		config         Config
		resolveOptions ResolveOptions
		expectedErr    string
	}{
		{
			Config{StartDelim: "{{hub"},
			ResolveOptions{},
			"the configurations StartDelim and StopDelim cannot be set independently",
		},
	}

	for _, test := range testcases {
		test := test

		testName := fmt.Sprintf("expectedErr=%s", test.expectedErr)
		t.Run("NewResolver: "+testName, func(t *testing.T) {
			t.Parallel()
			_, err := NewResolver(k8sConfig, test.config)
			if err == nil {
				t.Fatal("No error was provided")
			}

			if err.Error() != test.expectedErr {
				t.Fatalf("error \"%s\" != \"%s\"", err.Error(), test.expectedErr)
			}
		})

		t.Run("NewResolverWithCaching: "+testName, func(t *testing.T) {
			t.Parallel()

			ctx, cancelFunc := context.WithCancel(context.Background())
			defer cancelFunc()

			_, _, err := NewResolverWithCaching(ctx, k8sConfig, test.config)
			if err == nil {
				t.Fatal("No error was provided")
			}

			if err.Error() != test.expectedErr {
				t.Fatalf("error \"%s\" != \"%s\"", err.Error(), test.expectedErr)
			}
		})
	}
}

func TestValidateEncryptionFailures(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		resolveOptions ResolveOptions
		expectedErr    string
	}{
		{
			ResolveOptions{EncryptionConfig: EncryptionConfig{EncryptionEnabled: true}},
			"AESKey must be set to use this encryption mode",
		},
		{
			ResolveOptions{EncryptionConfig: EncryptionConfig{DecryptionEnabled: true}},
			"AESKey must be set to use this encryption mode",
		},
		{
			ResolveOptions{
				EncryptionConfig: EncryptionConfig{
					AESKey: bytes.Repeat([]byte{byte('A')}, 256/8), EncryptionEnabled: true,
				},
			},
			"initialization vector must be set to use this encryption mode",
		},
	}

	for _, test := range testcases {
		test := test

		testName := fmt.Sprintf("expectedErr=%s", test.expectedErr)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			err := validateEncryptionConfig(test.resolveOptions.EncryptionConfig)
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
	resolveOptions ResolveOptions
	ctx            interface{}
	expectedResult string
	expectedErr    error
}

func doResolveTest(t *testing.T, test resolveTestCase) {
	t.Helper()

	tmplStr := []byte(test.inputTmpl)

	if !test.resolveOptions.InputIsYAML {
		var err error

		tmplStr, err = yamlToJSON([]byte(test.inputTmpl))
		if err != nil {
			t.Fatalf(err.Error())
		}
	}

	resolver, err := NewResolver(k8sConfig, test.config)
	if err != nil {
		t.Fatalf(err.Error())
	}

	tmplResult, err := resolver.ResolveTemplate(tmplStr, test.ctx, &test.resolveOptions)

	if err != nil {
		if test.expectedErr == nil {
			t.Fatalf(err.Error())
		}

		if !(errors.Is(err, test.expectedErr) || strings.EqualFold(test.expectedErr.Error(), err.Error())) {
			t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
		}
	} else {
		val, err := JSONToYAML(tmplResult.ResolvedJSON)
		if err != nil {
			t.Fatalf(err.Error())
		}
		valStr := strings.TrimSuffix(string(val), "\n")

		if valStr != test.expectedResult {
			t.Fatalf("%s expected : '%s' , got : '%s'", test.inputTmpl, test.expectedResult, val)
		}
	}
}

func TestResolveTemplateWithCaching(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	resolver, _, err := NewResolverWithCaching(ctx, k8sConfig, Config{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	err = resolver.StartQueryBatch(client.ObjectIdentifier{})
	if err == nil || !strings.Contains(err.Error(), "SkipBatchManagement set to true") {
		t.Fatalf("Expected an error due SkipBatchManagement not being set to true but got %v", err)
	}

	tmplStr := `
data1: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'
data2: '{{ fromSecret "testns" "testsecret" "secretkey2" }}'
data3: '{{ .CustomVar }}'
data4: '{{ (lookup "v1" "Secret" "testns" "does-not-exist").data.key }}'
`

	tmplStrBytes, err := yamlToJSON([]byte(tmplStr))
	if err != nil {
		t.Fatalf(err.Error())
	}

	// No watcher should cause an error
	_, err = resolver.ResolveTemplate(tmplStrBytes, nil, nil)
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Expected ErrInvalidInput error but got %v", err)
	}

	watcher := client.ObjectIdentifier{
		Version:   "v1",
		Kind:      "ConfigMap",
		Namespace: "testns",
		Name:      "watcher",
	}

	templateCtx := struct{ CustomVar string }{}
	transformer := func(api CachingQueryAPI, templateCtx interface{}) (interface{}, error) {
		typedTemplateCtx, ok := templateCtx.(struct{ CustomVar string })
		if !ok {
			return templateCtx, nil
		}

		configMapGVK := schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}

		rv, err := api.Get(configMapGVK, "testns", "testcm-envc")
		if err != nil {
			return templateCtx, err
		}

		typedTemplateCtx.CustomVar = rv.GetLabels()["env"]

		return typedTemplateCtx, nil
	}

	resolveOptions := &ResolveOptions{
		Watcher:             &watcher,
		ContextTransformers: []func(CachingQueryAPI, interface{}) (interface{}, error){transformer},
	}

	result, err := resolver.ResolveTemplate(tmplStrBytes, templateCtx, resolveOptions)
	if err != nil {
		t.Fatal(err.Error())
	}

	expected := `{"data1":"c2VjcmV0a2V5MVZhbA==","data2":"c2VjcmV0a2V5MlZhbA==","data3":"c",` +
		`"data4":"\u003cno value\u003e"}`

	if string(result.ResolvedJSON) != expected {
		t.Fatalf("Unexpected template: %s", string(result.ResolvedJSON))
	}

	// One for the transformer and one for the lookup and one for the failed lookup
	if resolver.GetWatchCount() != 3 {
		t.Fatalf("Expected a watch count of 3 but got: %d", resolver.GetWatchCount())
	}

	cachedObjects, err := resolver.dynamicWatcher.ListWatchedFromCache(watcher)
	if err != nil {
		t.Fatal(err.Error())
	}

	if len(cachedObjects) != 2 {
		t.Fatalf("Expected two cached objectd but got %d", len(cachedObjects))
	}

	// Sort the slice so the output is consistent for the output validation
	sort.Slice(cachedObjects, func(i, j int) bool { return cachedObjects[i].GetName() < cachedObjects[j].GetName() })

	if cachedObjects[0].GetName() != "testcm-envc" {
		t.Fatalf("Expected the cached object of testcm-envc but got %s", cachedObjects[0].GetName())
	}

	if cachedObjects[1].GetName() != "testsecret" {
		t.Fatalf("Expected the cached object of testsecret but got %s", cachedObjects[1].GetName())
	}

	// Calling resolve template on the same template should not cause an error
	_, err = resolver.ResolveTemplate(tmplStrBytes, templateCtx, resolveOptions)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func TestStartQueryBatchNoCaching(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(k8sConfig, Config{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	err = resolver.StartQueryBatch(client.ObjectIdentifier{})
	if err == nil || !errors.Is(err, ErrCacheDisabled) {
		t.Fatalf("Expected an error due to the caching being disabled but got %v", err)
	}
}

func TestResolveTemplateWithCachingManualCleanUp(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	resolver, _, err := NewResolverWithCaching(ctx, k8sConfig, Config{SkipBatchManagement: true})
	if err != nil {
		t.Fatalf(err.Error())
	}

	tmplStr := `data1: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`

	tmplStrBytes, err := yamlToJSON([]byte(tmplStr))
	if err != nil {
		t.Fatalf(err.Error())
	}

	watcher := client.ObjectIdentifier{
		Version:   "v1",
		Kind:      "ConfigMap",
		Namespace: "testns",
		Name:      "watcher",
	}

	resolveOptions := &ResolveOptions{
		Watcher: &watcher,
	}

	err = resolver.StartQueryBatch(watcher)
	if err != nil {
		t.Fatal(err.Error())
	}

	result, err := resolver.ResolveTemplate(tmplStrBytes, nil, resolveOptions)
	if err != nil {
		t.Fatal(err.Error())
	}

	expected := `{"data1":"c2VjcmV0a2V5MVZhbA=="}`

	if string(result.ResolvedJSON) != expected {
		t.Fatalf("Unexpected template: %s", string(result.ResolvedJSON))
	}

	tmplStr2 := `data2: '{{ (lookup "v1" "Secret" "testns" "does-not-exist").data.key }}'`

	tmplStr2Bytes, err := yamlToJSON([]byte(tmplStr2))
	if err != nil {
		t.Fatalf(err.Error())
	}

	result2, err := resolver.ResolveTemplate(tmplStr2Bytes, nil, resolveOptions)
	if err != nil {
		t.Fatal(err.Error())
	}

	expected2 := `{"data2":"\u003cno value\u003e"}`

	if string(result2.ResolvedJSON) != expected2 {
		t.Fatalf("Unexpected template: %s", string(result.ResolvedJSON))
	}

	if resolver.GetWatchCount() != 2 {
		t.Fatalf("Expected two watches but got %d", resolver.GetWatchCount())
	}

	err = resolver.EndQueryBatch(watcher)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func TestResolveTemplateWithCachingNotAllowedClusterScoped(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	resolver, _, err := NewResolverWithCaching(ctx, k8sConfig, Config{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	tmplStr := `data1: '{{ lookup "v1" "Namespace" "" "some-namespace" }}'`

	tmplStrBytes, err := yamlToJSON([]byte(tmplStr))
	if err != nil {
		t.Fatalf(err.Error())
	}

	// No watcher should cause an error
	_, err = resolver.ResolveTemplate(
		tmplStrBytes,
		nil,
		&ResolveOptions{
			ClusterScopedAllowList: []ClusterScopedObjectIdentifier{
				{
					Group: "cluster.open-cluster-management.io",
					Kind:  "ManagedCluster",
					Name:  "local-cluster",
				},
			},
			LookupNamespace: "testns",
			Watcher: &client.ObjectIdentifier{
				Version:   "v1",
				Kind:      "ConfigMap",
				Namespace: "testns",
				Name:      "watcher",
			},
		},
	)
	if err == nil || !errors.As(err, &ClusterScopedLookupRestrictedError{}) {
		t.Fatalf("Expected ClusterScopedLookupRestrictedError error but got %v", err)
	}
}

func TestResolveTemplateWithCachingListQuery(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	resolver, _, err := NewResolverWithCaching(ctx, k8sConfig, Config{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	tmplStr := `data1: '{{ (index (lookup "v1" "ConfigMap" "testns" "" "env=a").items 0).data ` +
		`| mustToRawJson | toLiteral }}'`

	tmplStrBytes, err := yamlToJSON([]byte(tmplStr))
	if err != nil {
		t.Fatalf(err.Error())
	}

	watcher := client.ObjectIdentifier{
		Version:   "v1",
		Kind:      "ConfigMap",
		Namespace: "testns",
		Name:      "watcher",
	}

	result, err := resolver.ResolveTemplate(tmplStrBytes, nil, &ResolveOptions{Watcher: &watcher})
	if err != nil {
		t.Fatal(err.Error())
	}

	if string(result.ResolvedJSON) != `{"data1":{"cmkey1":"cmkey1Val"}}` {
		t.Fatalf("Unexpected template: %s", string(result.ResolvedJSON))
	}

	if resolver.GetWatchCount() != 1 {
		t.Fatalf("Expected a watch count of 1 but got: %d", resolver.GetWatchCount())
	}

	cachedObjects, err := resolver.dynamicWatcher.ListWatchedFromCache(watcher)
	if err != nil {
		t.Fatal(err.Error())
	}

	if len(cachedObjects) != 1 {
		t.Fatalf("Expected only one cached object but got %d", len(cachedObjects))
	}

	if cachedObjects[0].GetName() != "testcm-enva" {
		t.Fatalf("Expected the cached object of testcm-enva but got %s", cachedObjects[0].GetName())
	}

	// Calling resolve template on the same template should not cause an error
	_, err = resolver.ResolveTemplate(tmplStrBytes, nil, &ResolveOptions{Watcher: &watcher})
	if err != nil {
		t.Fatal(err.Error())
	}
}

type fakeReconciler struct{}

func (r fakeReconciler) Reconcile(_ context.Context, _ client.ObjectIdentifier) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func TestResolveTemplateWithPreexistingWatcher(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	fr := fakeReconciler{}

	dynWatcher, err := client.New(k8sConfig, fr, &client.Options{EnableCache: true})
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	resolver, err := NewResolverWithDynamicWatcher(dynWatcher, Config{SkipBatchManagement: true})
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	tmplStr := `data1: '{{ (lookup "v1" "ConfigMap" "testns" "testcm-enva").data ` +
		`| mustToRawJson | toLiteral }}'`

	tmplStrBytes, err := yamlToJSON([]byte(tmplStr))
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	watcher := client.ObjectIdentifier{
		Version:   "v1",
		Kind:      "ConfigMap",
		Namespace: "testns",
		Name:      "watcher",
	}

	_, err = resolver.ResolveTemplate(tmplStrBytes, nil, &ResolveOptions{Watcher: &watcher})
	if err == nil || !strings.Contains(err.Error(), "DynamicWatcher must be started") {
		t.Fatalf("Expected error requiring the DynamicWatcher to be started, got: %v", err)
	}

	go func() {
		_ = dynWatcher.Start(ctx)
	}()

	<-dynWatcher.Started()

	err = dynWatcher.StartQueryBatch(watcher)
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	defer func() {
		err := dynWatcher.EndQueryBatch(watcher)
		if err != nil {
			t.Fatalf("No error was expected: %v", err)
		}
	}()

	// Before resolving the template, do a get on that object through the DynamicWatcher
	// Then the test will validate that there is only one watch.
	configMapGVK := schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}

	_, err = dynWatcher.Get(watcher, configMapGVK, "testns", "testcm-enva")
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	result, err := resolver.ResolveTemplate(tmplStrBytes, nil, &ResolveOptions{Watcher: &watcher})
	if err != nil {
		t.Fatalf("No error was expected: %v", err)
	}

	if string(result.ResolvedJSON) != `{"data1":{"cmkey1":"cmkey1Val"}}` {
		t.Fatalf("Unexpected template: %s", string(result.ResolvedJSON))
	}

	if resolver.GetWatchCount() != 1 {
		t.Fatalf("Expected a watch count of 1 but got: %d", resolver.GetWatchCount())
	}
}

func TestResolveTemplateDefaultConfig(t *testing.T) {
	t.Parallel()

	testcases := map[string]resolveTestCase{
		"fromSecret": {
			inputTmpl:      `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			expectedResult: "data: c2VjcmV0a2V5MVZhbA==",
		},
		"fromSecret_duplicate_uses_resolve_cache": {
			inputTmpl: `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}` +
				`{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			expectedResult: "data: c2VjcmV0a2V5MVZhbA==c2VjcmV0a2V5MVZhbA==",
		},
		"fromConfigMap": {
			inputTmpl:      `param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			expectedResult: "param: cmkey1Val",
		},
		"toLiteral": {
			inputTmpl:      `param: '{{ fromConfigMap "testns" "testconfigmap" "ingressSources" | toLiteral }}'`,
			expectedResult: "param:\n  - 10.10.10.10\n  - 1.1.1.1",
		},
		"b64enc": {
			inputTmpl:      `config1: '{{ "testdata" | b64enc  }}'`,
			expectedResult: "config1: dGVzdGRhdGE=",
		},
		"base64enc": {
			inputTmpl:      `config1: '{{ "testdata" | base64enc  }}'`,
			expectedResult: "config1: dGVzdGRhdGE=",
		},
		"b64dec": {
			inputTmpl:      `config2: '{{ "dGVzdGRhdGE=" | b64dec  }}'`,
			expectedResult: "config2: testdata",
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
		"lookup_duplicate_list_uses_resolve_cache": {
			inputTmpl: `data: '{{ (index (lookup "v1" "ConfigMap" "testns" "" "env=a").items 0).data.cmkey1 }}` +
				`{{ (index (lookup "v1" "ConfigMap" "testns" "" "env=a").items 0).data.cmkey1 }}'`,
			expectedResult: "data: cmkey1Valcmkey1Val",
		},
		"copyConfigMapData": {
			inputTmpl: `data: '{{ copyConfigMapData "testns" "testconfigmap" }}'`,
			expectedResult: "data:\n  cmkey1: cmkey1Val\n  cmkey2: cmkey2Val\n" +
				"  ingressSources: '[10.10.10.10, 1.1.1.1]'",
		},
		"copySecretData": {
			inputTmpl:      `data: '{{ copySecretData "testns" "testsecret" }}'`,
			expectedResult: "data:\n  secretkey1: c2VjcmV0a2V5MVZhbA==\n  secretkey2: c2VjcmV0a2V5MlZhbA==",
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
			expectedErr: ErrInvalidContextType,
		},
		"invalid_context_nested_int": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         struct{ ClusterID int }{12},
			expectedErr: ErrInvalidContextType,
		},
		"invalid_context_map_with_int": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         struct{ Foo map[string]int }{Foo: map[string]int{"bar": 12}},
			expectedErr: ErrInvalidContextType,
		},
		"invalid_context_map_of_int": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         struct{ Foo map[int]string }{Foo: map[int]string{47: "something"}},
			expectedErr: ErrInvalidContextType,
		},
		"invalid_context_nested_struct": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         struct{ Metadata struct{ NestedInt int } }{struct{ NestedInt int }{NestedInt: 3}},
			expectedErr: ErrInvalidContextType,
		},
		"invalid_context_not_struct": {
			inputTmpl:   `test: '{{ printf "hello %s" "world" }}'`,
			ctx:         map[string]string{"hello": "world"},
			expectedErr: ErrInvalidContextType,
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
			config:      Config{},
			expectedErr: ErrMissingAPIResource,
		},
		"missing_api_resource_nested": {
			inputTmpl:   `value: '{{ index (lookup "v1" "NotAResource" "namespace" "object").data.list 2 }}'`,
			config:      Config{},
			expectedErr: ErrMissingAPIResource,
		},
		"context_transformers_without_caching": {
			inputTmpl: `param: '{{ "something" }}'`,
			resolveOptions: ResolveOptions{
				ContextTransformers: []func(CachingQueryAPI, interface{}) (interface{}, error){
					func(_ CachingQueryAPI, ctx interface{}) (interface{}, error) {
						return ctx, nil
					},
				},
			},
			expectedErr: fmt.Errorf(
				"%w: options.ContextTransformers cannot be set if caching is disabled",
				ErrInvalidInput,
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
			resolveOptions: ResolveOptions{InputIsYAML: true},
			expectedResult: "data: c2VjcmV0a2V5MVZhbA==",
		},
		"inputIsYAML_fromConfigMap": {
			inputTmpl:      `param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			resolveOptions: ResolveOptions{InputIsYAML: true},
			expectedResult: "param: cmkey1Val",
		},
		"inputIsYAML_base64dec": {
			inputTmpl:      `config2: '{{ "dGVzdGRhdGE=" | base64dec  }}'`,
			resolveOptions: ResolveOptions{InputIsYAML: true},
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

	type partialMetadata struct {
		Annotations map[string]string
		Labels      map[string]string
		Name        string
		Namespace   string
	}

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
		"nested_map": {
			inputTmpl:      `value: '{{ .Foo.greeting }}'`,
			ctx:            struct{ Foo map[string]string }{Foo: map[string]string{"greeting": "hello"}},
			expectedResult: "value: hello",
		},
		"nested_struct": {
			inputTmpl: `value: '{{ .Metadata.Labels.hello }} {{ .Metadata.Namespace }}'`,
			ctx: struct{ Metadata partialMetadata }{
				Metadata: partialMetadata{
					Labels: map[string]string{
						"hello": "world",
					},
					Namespace: "spacename",
				},
			},
			expectedResult: "value: world spacename",
		},
		"nested_map2": {
			inputTmpl: `value: '{{ .Metadata.labels.hello }} {{ .Metadata.namespace }}'`,
			ctx: struct{ Metadata map[string]interface{} }{
				Metadata: map[string]interface{}{
					"labels": map[string]string{
						"hello": "world",
					},
					"namespace": "spacename",
				},
			},
			expectedResult: "value: world spacename",
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

	encrypt := ResolveOptions{
		EncryptionConfig: EncryptionConfig{
			AESKey:               key,
			EncryptionEnabled:    true,
			InitializationVector: iv,
		},
	}

	decrypt := ResolveOptions{
		EncryptionConfig: EncryptionConfig{
			AESKey:               key,
			DecryptionEnabled:    true,
			InitializationVector: iv,
		},
	}

	testcases := map[string]resolveTestCase{
		"encrypt_protect": {
			inputTmpl:      `value: '{{ "Raleigh" | protect }}'`,
			resolveOptions: encrypt,
			expectedResult: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
		},
		"encrypt_protect_empty": {
			inputTmpl:      `value: '{{ "" | protect }}'`,
			resolveOptions: encrypt,
			expectedResult: `value: ""`,
		},
		"encrypt_fromSecret": {
			inputTmpl:      `data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			resolveOptions: encrypt,
			expectedResult: "data: $ocm_encrypted:c6PNhsEfbM9NRUqeJ+HbcECCyVdFnRbLdd+n8r1fS9M=",
		},
		"encrypt_copySecretData": {
			inputTmpl:      `data: '{{ copySecretData "testns" "testsecret" }}'`,
			resolveOptions: encrypt,
			expectedResult: "data:\n  secretkey1: $ocm_encrypted:c6PNhsEfbM9NRUqeJ+HbcECCyVdFnRbLdd+n8r1fS9M=\n" +
				"  secretkey2: $ocm_encrypted:VlXOhKuKGoHimHAYlQ2xz5EBw7mriqtt7fEP5ShP5cw=",
		},
		"decrypt": {
			inputTmpl:      "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			resolveOptions: decrypt,
			expectedResult: "value: Raleigh",
		},
		"decrypt_fallback_unused": {
			inputTmpl: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			resolveOptions: ResolveOptions{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, AESKeyFallback: otherKey, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: Raleigh",
		},
		"decrypt_fallback": {
			inputTmpl: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			resolveOptions: ResolveOptions{
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
			resolveOptions: ResolveOptions{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, DecryptionConcurrency: 5, DecryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: Raleigh\nvalue2: Raleigh2\nvalue3: Raleigh3",
		},
		"nothing_to_decrypt": {
			inputTmpl:      "value: Raleigh",
			resolveOptions: decrypt,
			expectedResult: "value: Raleigh",
		},
		"decrypt_multiline": {
			inputTmpl:      "value: $ocm_encrypted:x7Ix9DQueY+gf08PM6VSVA==",
			resolveOptions: decrypt,
			expectedResult: "value: Hello\\nRaleigh",
		},
		"decrypt_ignores_nonbase64": {
			inputTmpl:      "value: $ocm_encrypted:😱😱😱😱",
			resolveOptions: decrypt,
			expectedResult: `value: "$ocm_encrypted:\U0001F631\U0001F631\U0001F631\U0001F631"`,
		},
		"decryption_disabled": {
			inputTmpl:      "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			resolveOptions: encrypt, // decryptionEnabled defaults to false
			expectedResult: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
		},
		"encrypt_and_decrypt": {
			inputTmpl: "value: '{{ \"Raleigh\" | protect }}'\nvalue2: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==",
			resolveOptions: ResolveOptions{
				EncryptionConfig: EncryptionConfig{
					AESKey: key, DecryptionEnabled: true, EncryptionEnabled: true, InitializationVector: iv,
				},
			},
			expectedResult: "value: $ocm_encrypted:Eud/p3S7TvuP03S9fuNV+w==\nvalue2: Raleigh",
		},
		"protect_not_enabled": {
			inputTmpl:      `value: '{{ "Raleigh" | protect }}'`,
			resolveOptions: ResolveOptions{EncryptionConfig: EncryptionConfig{AESKey: key, InitializationVector: iv}},
			expectedErr:    ErrProtectNotEnabled,
		},
		"protect_not_enabled2": {
			inputTmpl:      `value: '{{ "Raleigh" | protect }}'`,
			resolveOptions: decrypt,
			expectedErr:    ErrProtectNotEnabled,
		},
		"encrypt_fails_illegalbase64": {
			inputTmpl:      "value: $ocm_encrypted:==========",
			resolveOptions: decrypt,
			expectedErr:    ErrInvalidB64OfEncrypted,
		},
		"encrypt_fails_invalidpaddinglength": {
			inputTmpl:      "value: $ocm_encrypted:mXIueuA3HvfBeobZZ0LdzA==",
			resolveOptions: decrypt,
			expectedErr:    ErrInvalidPKCS7Padding,
		},
		"encrypt_fails_invalidpaddingbytes": {
			inputTmpl:      "value: $ocm_encrypted:/X3LA2SczM7eqOLhZKAZXg==",
			resolveOptions: decrypt,
			expectedErr:    ErrInvalidPKCS7Padding,
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
		resolver, err := NewResolver(k8sConfig, test.config)
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
		configuredNamespace string
		actualNamespace     string
		returnedNamespace   string
		expectedError       error
	}{
		{"my-policies", "my-policies", "my-policies", nil},
		{"", "prod-configs", "prod-configs", nil},
		{"my-policies", "", "my-policies", nil},
		{
			"my-policies",
			"prod-configs",
			"",
			errors.New("the namespace argument is restricted to my-policies"),
		},
		{
			"policies",
			"prod-configs",
			"",
			errors.New("the namespace argument is restricted to policies"),
		},
	}

	for _, test := range tests {
		resolver, _ := NewResolver(k8sConfig, Config{})

		ns, err := resolver.getNamespace(test.actualNamespace, test.configuredNamespace)

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

//nolint:nosnakecase
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

	resolver, err := NewResolver(k8sConfig, Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to instantiate the templatesResolver struct: %v\n", err)
		panic(err)
	}

	templateContext := struct{ ClusterName string }{ClusterName: "cluster0001"}

	tmplResult, err := resolver.ResolveTemplate(policyJSON, templateContext, nil)
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
