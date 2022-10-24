// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"errors"
	"strings"
	"testing"
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
	}{
		{"testns", "v1", "ConfigMap", "testconfigmap", "", 1, nil},
		{"testns", "v1", "Secret", "testsecret", "", 1, nil},
		{"testns", "v1", "Secret", "idontexist", "", 0, nil},
		{
			"testns",
			"v1",
			"ConfigMap",
			"testconfigmap",
			"policies-ns",
			0,
			errors.New("the namespace argument passed to lookup is restricted to policies-ns"),
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

		if len(resolver.referencedObjects) != test.expectedObjCount {
			t.Fatalf("expected referenced object count: %d , got : %d",
				test.expectedObjCount, len(resolver.referencedObjects))
		} else if test.expectedObjCount != 0 {
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
