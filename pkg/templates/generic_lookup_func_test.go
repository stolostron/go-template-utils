// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/exp/slices"
)

func TestLookup(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		inputNs          string
		inputAPIVersion  string
		inputKind        string
		inputName        string
		lookupNamespace  string
		expectedObjCount int
		expectedErr      error
		expectedExists   bool
	}{
		{"testns", "v1", "ConfigMap", "testconfigmap", "", 1, nil, true},
		{"testns", "v1", "Secret", "testsecret", "", 1, nil, true},
		{"testns", "v1", "Secret", "idontexist", "", 1, nil, false},
		{
			"testns",
			"v1",
			"ConfigMap",
			"testconfigmap",
			"policies-ns",
			0,
			errors.New("the namespace argument passed to lookup is restricted to policies-ns"),
			false,
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(&k8sClient, k8sConfig, Config{LookupNamespace: test.lookupNamespace})
		if err != nil {
			t.Fatalf(err.Error())
		}

		val, err := resolver.lookup(test.inputAPIVersion, test.inputKind, test.inputNs, test.inputName)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
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

		if len(resolver.referencedObjects) != test.expectedObjCount {
			t.Fatalf("expected referenced object count: %d , got : %d",
				test.expectedObjCount, len(resolver.referencedObjects))
		} else if test.expectedExists && test.expectedObjCount != 0 {
			valMetadata := val["metadata"].(map[string]interface{})
			if val["apiVersion"] != test.inputAPIVersion || val["kind"] != test.inputKind ||
				valMetadata["name"] != test.inputName || valMetadata["namespace"] != test.inputNs {
				t.Fatalf(
					"expected:  ApiVersion= %s, Kind= %s, Name= %s, NS= %s,"+
						"Received: ApiVersion= %s, Kind= %s, Name= %s , NS= %s",
					test.inputAPIVersion, test.inputKind, test.inputName, test.inputNs,
					val["apiVersion"], val["kind"], valMetadata["name"], valMetadata["namespace"])
			}
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
		expectedObjCount int
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
			1,
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
			4,
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
			3,
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
			1,
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
			1,
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
			2,
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
			0,
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
			1,
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
			0,
			errors.New("unable to parse requirement: found 'IN', expected: in, notin, =, ==, !=, gt, lt"),
			false,
			nil,
		},
	}

	for _, test := range testcases {
		resolver, err := NewResolver(&k8sClient, k8sConfig, Config{LookupNamespace: test.lookupNamespace})
		if err != nil {
			t.Fatalf(err.Error())
		}

		// prevent linter critic for unslicing.  this is required to duplicate passing multiple args as a user would
		//nolint:gocritic
		val, err := resolver.lookup(test.inputAPIVersion,
			test.inputKind,
			test.inputNs,
			test.inputName,
			test.labelSelector[:]...)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
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

		if len(resolver.referencedObjects) != test.expectedObjCount {
			t.Fatalf("expected referenced object count: %d , got : %d",
				test.expectedObjCount, len(resolver.referencedObjects))
		} else if test.expectedExists && test.inputName != "" {
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
