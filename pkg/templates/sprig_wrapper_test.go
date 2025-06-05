// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"fmt"
	"os"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

func TestGetSprigFunc(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		template       string
		expectedResult string
	}{
		"add": {
			`{{ add 2 2 }}`,
			"4",
		},
		"append": {
			`{{ append (list 1 2 3) 4 }}`,
			"[1 2 3 4]",
		},
		"cat": {
			`{{ cat "Foo" "Bar" }}`,
			"Foo Bar",
		},
		"concat": {
			`{{ concat (list 1 2 3) (list 4 5 6) }}`,
			"[1 2 3 4 5 6]",
		},
		"contains": {
			`{{ contains "Foo" "Foo Bar" }}`,
			"true",
		},
		"default": {
			`{{ $a := "Foo Bar" }}{{ default "foo" $a }}`,
			"Foo Bar",
		},
		"dict": {
			`{{ dict "name1" "value1" "name2" "value2" "name3" "value 3" }}`,
			"map[name1:value1 name2:value2 name3:value 3]",
		},
		"dig": {
			`{{ dig "user" "role" "foo" "default" (fromJson "{\"user\": {\"role\": {\"foo\": \"bar\"}}}") }}`,
			"bar",
		},
		"div": {
			`{{ div 4 2 }}`,
			"2",
		},
		"empty": {
			`{{ $a := "Foo Bar" }}{{ empty $a }}`,
			"false",
		},
		"fromJson": {
			`{{ $a := fromJson "{\"foo\": \"Bar\"}" }}{{ $a.foo }}`,
			"Bar",
		},
		"fromJSON": {
			`{{ $a := fromJSON "{\"foo\": \"Bar\"}" }}{{ $a.foo }}`,
			"Bar",
		},
		"get": {
			`{{ get (dict "key1" "value1") "key1" }}`,
			"value1",
		},
		"has": {
			`{{ has 2 (list 1 2 3) }}`,
			"true",
		},
		"hasKey": {
			`{{ hasKey (dict "name1" "value1") "name1" }}`,
			"true",
		},
		"hasPrefix": {
			`{{ hasPrefix "Foo" "FooBar" }}`,
			"true",
		},
		"hasSuffix": {
			`{{ hasSuffix "Bar" "FooBar" }}`,
			"true",
		},
		"htpasswd": {
			`{{ empty (htpasswd "foo" "bar") }}`,
			"false",
		},
		"join": {
			`{{ list "Foo" "Bar" | join "_" }}`,
			"Foo_Bar",
		},
		// Pass to sortAlpha because the order returned isn't guaranteed
		"keys": {
			`{{ keys (dict "key1" "value1" "key2" "values2") | sortAlpha }}`,
			"[key1 key2]",
		},
		"list": {
			`{{ list "Foo" "Bar" }}`,
			"[Foo Bar]",
		},
		"lower": {
			`{{ lower "Foo Bar" }}`,
			"foo bar",
		},
		"merge": {
			`{{ merge (dict "name1" "value1") (dict "name1" "value1" "name2" "value2") }}`,
			"map[name1:value1 name2:value2]",
		},
		"mul": {
			`{{ mul 2 2 }}`,
			"4",
		},
		"mustAppend": {
			`{{ mustAppend (list 1 2 3) 4 }}`,
			"[1 2 3 4]",
		},
		"mustFromJson": {
			`{{ $a := mustFromJson "{\"foo\": \"Bar\"}" }}{{ $a.foo }}`,
			"Bar",
		},
		"mustFromJSON": {
			`{{ $a := mustFromJSON "{\"foo\": \"Bar\"}" }}{{ $a.foo }}`,
			"Bar",
		},
		"mustHas": {
			`{{ mustHas 2 (list 1 2 3) }}`,
			"true",
		},
		"mustMerge": {
			`{{ mustMerge (dict "name1" "value1") (dict "name3" (list 1 2 3)) }}`,
			"map[name1:value1 name3:[1 2 3]]",
		},
		"mustPrepend": {
			`{{ mustPrepend (list 1 2 3) 4 }}`,
			"[4 1 2 3]",
		},
		"mustRegexFind": {
			`{{ mustRegexFind "[a-zA-Z]{2}[1-9]{2}" "AbCd1234" }}`,
			"Cd12",
		},
		"mustRegexFindAll": {
			`{{ mustRegexFindAll "[1,3,5,7]" "123456789" -1 }}`,
			"[1 3 5 7]",
		},
		"mustRegexMatch": {
			`{{ mustRegexMatch "^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}$" "test@acme@com" }}`,
			"false",
		},
		"mustSlice": {
			`{{ mustSlice (list 1 2 3) 1 3 }}`,
			"[2 3]",
		},
		"mustToDate": {
			`{{ mustToDate "2006-01-02" "2023-12-31" | date "01/02/2006" }}`,
			"12/31/2023",
		},
		"mustToJson": {
			`{{ mustToJson .Labels }}`,
			`{"hello":"world"}`,
		},
		"mustToJSON": {
			`{{ mustToJSON .Labels }}`,
			`{"hello":"world"}`,
		},
		"mustToRawJson": {
			`{{ mustToRawJson .Labels }}`,
			`{"hello":"world"}`,
		},
		"mustToRawJSON": {
			`{{ mustToRawJSON .Labels }}`,
			`{"hello":"world"}`,
		},
		"prepend": {
			`{{ prepend (list 1 2 3) 4 }}`,
			"[4 1 2 3]",
		},
		"quote": {
			`{{ "Foo Bar" | quote }}`,
			"\"Foo Bar\"",
		},
		"regexFind": {
			`{{ regexFind "[a-zA-Z][1-9]" "abcd1234" }}`,
			"d1",
		},
		"regexFindAll": {
			`{{ regexFindAll "[2,4,6,8]" "123456789" -1 }}`,
			"[2 4 6 8]",
		},
		"regexMatch": {
			`{{ regexMatch "^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}$" "test@acme.com" }}`,
			"true",
		},
		"regexQuoteMeta": {
			`{{ regexQuoteMeta "1.2.3" }}`,
			`1\.2\.3`,
		},
		"replace": {
			`{{ "Foo Bar" | replace " " "-" }}`,
			"Foo-Bar",
		},
		"round": {
			`{{ round 123.55555 0 }}`,
			"124",
		},
		"semver": {
			`{{ $a := semver "4.10.22" }}{{ $a.Minor }}`,
			"10",
		},
		"semverCompare": {
			`{{ semverCompare "^1.2.0" "1.2.3" }}`,
			"true",
		},
		"set": {
			`{{ set (dict) "name4" "value4" }}`,
			"map[name4:value4]",
		},
		"slice": {
			`{{ slice (list 1 2 3) 1 3 }}`,
			"[2 3]",
		},
		"sortAlpha": {
			`{{ sortAlpha (list "api" "test" "example" "com") }}`,
			"[api com example test]",
		},
		"split": {
			`{{ $a := split "." "api.test.example.com" }}{{ $a._1 }}`,
			"test",
		},
		"splitn": {
			`{{ $a := splitn "." 3 "api.test.example.com" }}{{ $a._2 }}`,
			"example.com",
		},
		"splitList": {
			`{{ splitList "." "api.test.example.com" }}`,
			"[api test example com]",
		},
		"sub": {
			`{{ sub 4 2 }}`,
			"2",
		},
		"substr": {
			`{{ substr 0 3 "foo bar" }}`,
			"foo",
		},
		"ternary": {
			`{{ $a := true }}{{ ternary "Foo" "Bar" $a }}`,
			"Foo",
		},
		"toDate": {
			`{{ toDate "2006-01-02" "2023-12-31" | date "01/02/2006" }}`,
			"12/31/2023",
		},
		"toJson": {
			`{{ toJson .Labels }}`,
			`{"hello":"world"}`,
		},
		"toJSON": {
			`{{ toJSON .Labels }}`,
			`{"hello":"world"}`,
		},
		"toRawJson": {
			`{{ toRawJson .Labels }}`,
			`{"hello":"world"}`,
		},
		"toRawJSON": {
			`{{ toRawJSON .Labels }}`,
			`{"hello":"world"}`,
		},
		"trim": {
			`{{ trim "  foo bar  " }}`,
			"foo bar",
		},
		"trimAll": {
			`{{ trimAll "-" "-foo bar-" }}`,
			"foo bar",
		},
		"trunc": {
			`{{ trunc 3 "foo bar" }}`,
			"foo",
		},
		"unset": {
			`{{ unset (dict "name2" "value2" "name4" "value4") "name4" }}`,
			"map[name2:value2]",
		},
		"until": {
			`{{ until 3 }}`,
			"[0 1 2]",
		},
		"untilStep": {
			`{{ untilStep 1 6 2 }}`,
			"[1 3 5]",
		},
		"upper": {
			`{{ upper "foo bar" }}`,
			"FOO BAR",
		},
		// Pass to sortAlpha because the order returned isn't guaranteed
		"values": {
			`{{ values (dict "key1" "value1" "key2" "value2") | sortAlpha }}`,
			"[value1 value2]",
		},
	}

	for funcName, test := range tests {
		t.Run(funcName, func(t *testing.T) {
			t.Parallel()

			resolver, _ := NewResolver(k8sConfig, Config{})

			policyYAML := `
---
data:
  %s: '%s'
`

			policyJSON, err := yamlToJSON([]byte(fmt.Sprintf(policyYAML, funcName, test.template)))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to convert the policy YAML to JSON: %v\n", err)
				panic(err)
			}

			templateCtx := struct{ Labels map[string]string }{map[string]string{"hello": "world"}}

			resolvedResult, err := resolver.ResolveTemplate(policyJSON, templateCtx, nil)
			if err != nil {
				t.Fatalf("Failed to process the policy YAML: %v\n", err)
			}

			policyResolvedJSON := resolvedResult.ResolvedJSON
			var policyResolved interface{}
			err = yaml.Unmarshal(policyResolvedJSON, &policyResolved)

			data, ok := policyResolved.(map[string]interface{})["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("Failed to process the policy YAML reading data key: %v\n", err)
			}

			actualValue, ok := data[funcName].(string)
			if !ok {
				t.Fatalf("Failed testing %s: %v\n", funcName, err)
			}

			if actualValue != test.expectedResult {
				t.Fatalf("Test %s failed. expected : %v , got : %v", funcName, test.expectedResult, actualValue)
			}
		})
	}
}

func TestSprigFuncErr(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		template    string
		expectedErr string
	}{
		"fail": {
			`{{ fail "total fail" }}`,
			`error calling fail: total fail`,
		},
	}

	for funcName, test := range tests {
		t.Run(funcName, func(t *testing.T) {
			t.Parallel()

			resolver, _ := NewResolver(k8sConfig, Config{})

			policyYAML := `
---
data:
  %s: '%s'
`

			policyJSON, err := yamlToJSON([]byte(fmt.Sprintf(policyYAML, funcName, test.template)))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to convert the policy YAML to JSON: %v\n", err)
				panic(err)
			}

			templateCtx := struct{ Labels map[string]string }{map[string]string{"hello": "world"}}

			_, err = resolver.ResolveTemplate(policyJSON, templateCtx, nil)
			if err != nil {
				if !strings.HasSuffix(err.Error(), test.expectedErr) {
					t.Fatalf("\nTest %s failed. expected error suffix : %s\n", funcName, test.expectedErr)
				}
			} else {
				t.Fatalf(
					"Test %s failed because it returned no error. Expected error : %s\n", funcName, test.expectedErr)
			}
		})
	}
}
