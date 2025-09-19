// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stolostron/kubernetes-dependency-watches/client"
)

func TestLookup(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		inputNs         string
		inputAPIVersion string
		inputKind       string
		inputName       string
		lookupNamespace string
		expectedErr     error
		expectedExists  bool
	}{
		{"testns", "v1", "ConfigMap", "testconfigmap", "", nil, true},
		{"testns", "v1", "Secret", "testsecret", "", nil, true},
		{"testns", "v1", "Secret", "idontexist", "", nil, false},
		{
			"testns",
			"v1",
			"ConfigMap",
			"testconfigmap",
			"policies-ns",
			errors.New("the namespace argument is restricted to policies-ns"),
			false,
		},
		{
			"testns",
			"",
			"ConfigMap",
			"testconfigmap",
			"",
			errors.New("the apiVersion and kind are required"),
			false,
		},
		{
			"testns",
			"v1",
			"",
			"testconfigmap",
			"",
			errors.New("the apiVersion and kind are required"),
			false,
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(k8sConfig, Config{})
		if err != nil {
			t.Fatal(err)
		}

		templateResult := &TemplateResult{}

		val, err := resolver.lookup(
			&ResolveOptions{LookupNamespace: test.lookupNamespace},
			templateResult,
			test.inputAPIVersion,
			test.inputKind,
			test.inputNs,
			test.inputName,
		)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatal(err)
			}

			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else if test.expectedErr != nil {
			t.Fatalf("An error was expected but not returned %s", test.expectedErr)
		}

		if test.expectedExists {
			if len(val) == 0 {
				t.Fatal("An object was expected but not returned")
			}

			if test.inputKind == "Secret" && !templateResult.HasSensitiveData {
				t.Fatalf("expected HasSensitiveData to be set to true")
			}
		} else if len(val) != 0 {
			t.Fatal("An object was unexpected but one was returned")
		}
	}
}

func TestLookupWithLabels(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		inputNs          string
		inputAPIVersion  string
		inputKind        string
		inputName        string
		lookupNamespace  string
		labelSelector    []string
		expectedErr      error
		expectedExists   bool
		expectedObjNames []string
	}{
		{
			"testns",
			"v1",
			"ConfigMap",
			"testcm-envc",
			"",
			nil,
			nil,
			true,
			[]string{"testcm-envc"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			nil,
			nil,
			true,
			[]string{"testconfigmap", "testcm-enva", "testcm-envb", "testcm-envc"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"app=test"},
			nil,
			true,
			[]string{"testcm-enva", "testcm-envb", "testcm-envc"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"env=a"},
			nil,
			true,
			[]string{"testcm-enva"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"env in (a)"},
			nil,
			true,
			[]string{"testcm-enva"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"env in (a,b)"},
			nil,
			true,
			[]string{"testcm-enva", "testcm-envb"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"env in (d)"},
			nil,
			true, // Note ExpectedObject = true as lookup returns empty list
			nil,
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"app=test", "env in (c)"},
			nil,
			true,
			[]string{"testcm-envc"},
		},
		{
			"testns",
			"v1",
			"ConfigMap",
			"",
			"",
			[]string{"env IN (a)"},
			errors.New("unable to parse requirement: found 'IN', expected: in, notin, =, ==, !=, gt, lt"),
			false,
			nil,
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(k8sConfig, Config{})
		if err != nil {
			t.Fatal(err)
		}

		// prevent linter critic for unslicing.  this is required to duplicate passing multiple args as a user would
		//nolint:gocritic
		val, err := resolver.lookup(
			&ResolveOptions{LookupNamespace: test.lookupNamespace},
			nil,
			test.inputAPIVersion,
			test.inputKind,
			test.inputNs,
			test.inputName,
			test.labelSelector[:]...,
		)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatal(err)
			}

			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else if test.expectedErr != nil {
			t.Fatalf("An error was expected but not returned %s", test.expectedErr)
		}

		if test.expectedExists {
			if len(val) == 0 {
				t.Fatal("An object was expected but not returned")
			}
		} else if len(val) != 0 {
			t.Fatalf("An object was unexpected but one was returned: %v", test)
		}

		if test.expectedExists && test.inputName != "" {
			valMetadata := val["metadata"].(map[string]interface{})
			if val["apiVersion"] != test.inputAPIVersion || val["kind"] != test.inputKind ||
				valMetadata["name"] != test.inputName || valMetadata["namespace"] != test.inputNs {
				t.Fatalf(
					"expected:  ApiVersion= %s, Kind= %s, Name= %s, NS= %s,"+
						"Received: ApiVersion= %s, Kind= %s, Name= %s , NS= %s",
					test.inputAPIVersion, test.inputKind, test.inputName, test.inputNs,
					val["apiVersion"], val["kind"], valMetadata["name"], valMetadata["namespace"])
			}
		} else if test.expectedExists && test.inputName == "" {
			for _, lstObj := range val["items"].([]interface{}) {
				refObject := lstObj.(map[string]interface{})
				refObjMetadata := refObject["metadata"].(map[string]interface{})

				if refObject["apiVersion"] != test.inputAPIVersion || refObject["kind"] != test.inputKind ||
					refObjMetadata["namespace"] != test.inputNs {
					t.Fatalf(
						"expected:  ApiVersion= %s, Kind= %s, NS= %s,"+
							"Received: ApiVersion= %s, Kind= %s, NS= %s",
						test.inputAPIVersion, test.inputKind, test.inputNs,
						refObject["apiVersion"], refObject["kind"], refObjMetadata["namespace"])
				}

				// Verify the objects returned by label match the name(s) we expect
				if len(test.expectedObjNames) > 0 &&
					!(slices.Contains(test.expectedObjNames, fmt.Sprintf("%v", refObjMetadata["name"]))) {
					t.Fatalf("Lookup returned %v, not found in %v", refObjMetadata["name"], test.expectedObjNames)
				}
			}
		}
	}
}

func TestLookupClusterScoped(t *testing.T) {
	t.Parallel()

	clusterScopedErr := ClusterScopedLookupRestrictedError{"Node", "foo"}

	testcases := []struct {
		inputNs         string
		inputAPIVersion string
		inputKind       string
		inputName       string
		lookupNamespace string
		allowlist       []ClusterScopedObjectIdentifier
		expectedErr     error
		expectedExists  bool
	}{
		// No allowlist
		{"", "v1", "Node", "foo", "", nil, nil, false},
		{"policies-ns", "v1", "Node", "foo", "", nil, nil, false},
		{"", "v1", "Node", "foo", "policies-ns", nil, clusterScopedErr, false},
		{"policies-ns", "v1", "Node", "foo", "policies-ns", nil, clusterScopedErr, false},
		// With an allowlist matching the resource
		{
			"",
			"v1",
			"Node",
			"foo",
			"policies-ns",
			[]ClusterScopedObjectIdentifier{{"*", "*", "*"}},
			nil,
			false,
		},
		{
			"",
			"v1",
			"Node",
			"foo",
			"policies-ns",
			[]ClusterScopedObjectIdentifier{{"", "Node", "*"}},
			nil,
			false,
		},
		{
			"",
			"v1",
			"Node",
			"foo",
			"policies-ns",
			[]ClusterScopedObjectIdentifier{{"", "Node", "foo"}},
			nil,
			false,
		},
		// With an allowlist not matching the resource
		{
			"",
			"v1",
			"Node",
			"foo",
			"policies-ns",
			[]ClusterScopedObjectIdentifier{{"", "Node", "bar"}},
			clusterScopedErr,
			false,
		},
		{
			"",
			"v1",
			"Node",
			"foo",
			"policies-ns",
			[]ClusterScopedObjectIdentifier{{"myapi.com", "Node", "foo"}},
			clusterScopedErr,
			false,
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(k8sConfig, Config{})
		if err != nil {
			t.Fatal(err)
		}

		templateResult := &TemplateResult{}

		val, err := resolver.lookup(
			&ResolveOptions{
				LookupNamespace:        test.lookupNamespace,
				ClusterScopedAllowList: test.allowlist,
			},
			templateResult,
			test.inputAPIVersion,
			test.inputKind,
			test.inputNs,
			test.inputName,
		)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatal(err)
			}

			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else if test.expectedErr != nil {
			t.Fatalf("An error was expected but not returned %s", test.expectedErr)
		}

		if test.expectedExists {
			if len(val) == 0 {
				t.Fatal("An object was expected but not returned")
			}
		} else if len(val) != 0 {
			t.Fatal("An object was unexpected but one was returned")
		}

		if templateResult.HasSensitiveData {
			t.Fatalf("expected HasSensitiveData to be set to false")
		}
	}
}

func TestGetNodesWithExactRoles(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		roleNames        []string
		expectedErr      error
		expectedExists   bool
		expectedObjNames []string
	}{
		{
			[]string{"infra"},
			nil,
			true,
			[]string{"node-infra1", "node-infra2"},
		},
		{
			[]string{"storage"},
			nil,
			false,
			nil,
		},
		{
			[]string{"infra", "storage"},
			nil,
			true,
			[]string{"node-storage"},
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(k8sConfig, Config{})
		if err != nil {
			t.Fatal(err)
		}

		templateResult := &TemplateResult{}

		val, err := resolver.getNodesWithExactRoles(
			&ResolveOptions{
				LookupNamespace:        "",
				ClusterScopedAllowList: nil,
			},
			templateResult,
			test.roleNames...,
		)
		if err != nil {
			t.Fatal(err)
		}

		if len(val["items"].([]interface{})) == 0 && test.expectedExists {
			t.Fatal("An object was expected but not returned")
		} else {
			for _, lstObj := range val["items"].([]interface{}) {
				refObject := lstObj.(map[string]interface{})
				refObjMetadata := refObject["metadata"].(map[string]interface{})

				if !slices.Contains(test.expectedObjNames, refObjMetadata["name"].(string)) {
					t.Fatalf(
						"Received: %s"+
							"expected node name:  %v,",
						refObjMetadata["name"], test.expectedObjNames)
				}
			}
		}

		if templateResult.HasSensitiveData {
			t.Fatalf("expected HasSensitiveData to be set to false")
		}
	}
}

func TestHasNodesWithExactRoles(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(k8sConfig, Config{})
	if err != nil {
		t.Fatal(err)
	}

	testRole := []string{"infra"}

	val, err := resolver.hasNodesWithExactRoles(
		&ResolveOptions{
			LookupNamespace:        "",
			ClusterScopedAllowList: nil,
		},
		testRole...,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !val {
		t.Fatal("Infra nodes should exist, but returned false")
	}
}

func TestLookupOfUnwatchableKind(t *testing.T) {
	t.Parallel()

	plainResolver, err := NewResolver(k8sConfig, Config{})
	if err != nil {
		t.Fatal(err)
	}

	val, err := plainResolver.lookup(
		&ResolveOptions{},
		&TemplateResult{},
		"v1",
		"ComponentStatus",
		"",
		"scheduler",
	)
	if err != nil {
		t.Logf("Expected nil error, got: %v", err)
		t.Fail()
	}

	if _, ok := val["metadata"]; !ok {
		t.Logf("Expected metadata in lookup response, got: %v", val)
		t.Fail()
	}

	ctx, cancelFunc := context.WithCancel(t.Context())
	defer cancelFunc()

	cachingResolver, _, err := NewResolverWithCaching(ctx, k8sConfig, Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = cachingResolver.lookup(
		&ResolveOptions{},
		&TemplateResult{},
		"v1",
		"ComponentStatus",
		"",
		"scheduler",
	)
	if err == nil {
		t.Log("Expected error, got nil")
		t.Fail()
	}

	if !errors.Is(err, client.ErrResourceUnwatchable) {
		t.Logf("Expected an Unwatchable error, got: %v", err)
		t.Fail()
	}
}
