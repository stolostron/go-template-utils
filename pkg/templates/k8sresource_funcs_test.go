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
	"errors"
	"strings"
	"testing"
)

func TestFromSecret(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		inputNs        string
		inputCMname    string
		inputKey       string
		expectedResult string
		expectedErr    error
	}{
		{"testns", "testsecret", "secretkey1", "secretkey1Val", nil}, // green-path test
		{"testns", "testsecret", "secretkey2", "secretkey2Val", nil}, // green-path test
		{
			"testns",
			"idontexist",
			"secretkey1",
			"secretkey2Val",
			errors.New(`failed to get the secret idontexist from testns: secrets "idontexist" not found`),
		}, // error : nonexistant secret
		{"testns", "testsecret", "blah", "", nil}, // error : nonexistant key
	}

	for _, test := range testcases {
		val, err := fromSecret(test.inputNs, test.inputCMname, test.inputKey)

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
		inputNs        string
		inputCMname    string
		inputKey       string
		expectedResult string
		expectedErr    error
	}{
		{"testns", "testconfigmap", "cmkey1", "cmkey1Val", nil},
		{"testns", "testconfigmap", "cmkey2", "cmkey2Val", nil},
		{"testns", "idontexist", "cmkey1", "cmkey1Val", errors.New(`failed getting the ConfigMap idontexist from testns: configmaps "idontexist" not found`)},
		{"testns", "testconfigmap", "idontexist", "", nil},
	}

	for _, test := range testcases {
		val, err := fromConfigMap(test.inputNs, test.inputCMname, test.inputKey)

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
