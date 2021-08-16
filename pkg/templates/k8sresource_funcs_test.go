// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

func TestFromSecret(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		inputNs         string
		inputCMname     string
		inputKey        string
		lookupNamespace string
		expectedResult  string
		expectedErr     error
	}{
		{"testns", "testsecret", "secretkey1", "", "secretkey1Val", nil},       // green-path test
		{"testns", "testsecret", "secretkey2", "", "secretkey2Val", nil},       // green-path test
		{"testns", "testsecret", "secretkey2", "testns", "secretkey2Val", nil}, // green-path test
		{
			"testns",
			"idontexist",
			"secretkey1",
			"",
			"secretkey2Val",
			errors.New(`failed to get the secret idontexist from testns: secrets "idontexist" not found`),
		}, // error : nonexistant secret
		{"testns", "testsecret", "blah", "", "", nil}, // error : nonexistant key
		{
			"testns",
			"testsecret",
			"secretkey2",
			"policies-ns",
			"",
			errors.New("the namespace argument passed to fromSecret is restricted to policies-ns"),
		}, // error : restricted input namespace
	}

	for _, test := range testcases {
		resolver := getTemplateResolver(Config{KubeConfig: &rest.Config{}, LookupNamespace: test.lookupNamespace})
		val, err := resolver.fromSecret(test.inputNs, test.inputCMname, test.inputKey)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}
			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else if val != base64encode(test.expectedResult) {
			t.Fatalf("expected : %s , got : %s", base64encode(test.expectedResult), val)
		}
	}
}

func TestFromConfigMap(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		inputNs         string
		inputCMname     string
		inputKey        string
		lookupNamespace string
		expectedResult  string
		expectedErr     error
	}{
		{"testns", "testconfigmap", "cmkey1", "", "cmkey1Val", nil},
		{"testns", "testconfigmap", "cmkey2", "", "cmkey2Val", nil},
		{"testns", "testconfigmap", "cmkey2", "testns", "cmkey2Val", nil},
		{"testns", "idontexist", "cmkey1", "", "cmkey1Val", errors.New(`failed getting the ConfigMap idontexist from testns: configmaps "idontexist" not found`)},
		{"testns", "testconfigmap", "idontexist", "", "", nil},
		{
			"testns",
			"testconfigmap",
			"cmkey1",
			"policies-ns",
			"cmkey1Val",
			errors.New("the namespace argument passed to fromConfigMap is restricted to policies-ns"),
		},
	}

	for _, test := range testcases {
		resolver := getTemplateResolver(Config{KubeConfig: &rest.Config{}, LookupNamespace: test.lookupNamespace})
		val, err := resolver.fromConfigMap(test.inputNs, test.inputCMname, test.inputKey)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}
			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else if val != test.expectedResult {
			t.Fatalf("expected : %s , got : %s", test.expectedResult, val)
		}
	}
}
