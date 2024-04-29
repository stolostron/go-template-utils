// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"fmt"
	"os"
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
		"has": {
			`{{ has 2 (list 1 2 3) }}`,
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
		"list": {
			`{{ list "Foo" "Bar" }}`,
			"[Foo Bar]",
		},
		"lower": {
			`{{ lower "Foo Bar" }}`,
			"foo bar",
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
		"mustHas": {
			`{{ mustHas 2 (list 1 2 3) }}`,
			"true",
		},
		"mustPrepend": {
			`{{ mustPrepend (list 1 2 3) 4 }}`,
			"[4 1 2 3]",
		},
		"mustSlice": {
			`{{ mustSlice (list 1 2 3) 1 3 }}`,
			"[2 3]",
		},
		"mustToDate": {
			`{{ mustToDate "2006-01-02" "2023-12-31" | date "01/02/2006" }}`,
			"12/31/2023",
		},
		"mustToRawJson": {
			`{{ mustToRawJson .Labels }}`,
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
		"slice": {
			`{{ slice (list 1 2 3) 1 3 }}`,
			"[2 3]",
		},
		"split": {
			`{{ $a := split "." "api.test.example.com" }}{{ $a._1 }}`,
			"test",
		},
		"splitn": {
			`{{ $a := splitn "." 3 "api.test.example.com" }}{{ $a._2 }}`,
			"example.com",
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
		"toRawJson": {
			`{{ toRawJson .Labels }}`,
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
	}

	for funcName, test := range tests {
		funcName := funcName
		test := test

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
