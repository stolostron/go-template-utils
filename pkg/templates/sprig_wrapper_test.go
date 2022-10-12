// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"fmt"
	"os"
	"testing"

	yaml "gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestGetSprigFunc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		funcName       string
		template       string
		expectedResult string
	}{
		{
			"trim",
			`{{ trim "  foo bar  " }}`,
			"foo bar",
		},
		{
			"upper",
			`{{ upper "foo bar" }}`,
			"FOO BAR",
		},
		{
			"lower",
			`{{ lower "Foo Bar" }}`,
			"foo bar",
		},
		{
			"contains",
			`{{ contains "Foo" "Foo Bar" }}`,
			"true",
		},
		{
			"hasPrefix",
			`{{ hasPrefix "Foo" "FooBar" }}`,
			"true",
		},
		{
			"hasSuffix",
			`{{ hasSuffix "Bar" "FooBar" }}`,
			"true",
		},
		{
			"quote",
			`{{ "Foo Bar" | quote }}`,
			"\"Foo Bar\"",
		},
		{
			"cat",
			`{{ cat "Foo" "Bar" }}`,
			"Foo Bar",
		},
		{
			"replace",
			`{{ "Foo Bar" | replace " " "-" }}`,
			"Foo-Bar",
		},
		{
			"split",
			`{{ $a := split "." "api.test.example.com" }}{{ $a._1 }}`,
			"test",
		},
		{
			"splitn",
			`{{ $a := splitn "." 3 "api.test.example.com" }}{{ $a._2 }}`,
			"example.com",
		},
		{
			"list",
			`{{ list "Foo" "Bar" }}`,
			"[Foo Bar]",
		},
		{
			"join",
			`{{ list "Foo" "Bar" | join "_" }}`,
			"Foo_Bar",
		},
		{
			"until",
			`{{ until 3 }}`,
			"[0 1 2]",
		},
		{
			"untilStep",
			`{{ untilStep 1 6 2 }}`,
			"[1 3 5]",
		},
		{
			"default",
			`{{ $a := "Foo Bar" }}{{ default "foo" $a }}`,
			"Foo Bar",
		},
		{
			"empty",
			`{{ $a := "Foo Bar" }}{{ empty $a }}`,
			"false",
		},
		{
			"fromJson",
			`{{ $a := fromJson "{\"foo\": \"Bar\"}" }}{{ $a.foo }}`,
			"Bar",
		},
		{
			"mustFromJson",
			`{{ $a := mustFromJson "{\"foo\": \"Bar\"}" }}{{ $a.foo }}`,
			"Bar",
		},
		{
			"ternary",
			`{{ $a := true }}{{ ternary "Foo" "Bar" $a }}`,
			"Foo",
		},
		{
			"semver",
			`{{ $a := semver "4.10.22" }}{{ $a.Minor }}`,
			"10",
		},
		{
			"semverCompare",
			`{{ semverCompare "^1.2.0" "1.2.3" }}`,
			"true",
		},
	}

	for _, test := range tests {
		var k8sClient kubernetes.Interface = fake.NewSimpleClientset()

		resolver, _ := NewResolver(&k8sClient, &rest.Config{}, Config{})

		policyYAML := `
---
data:
  %s: '%s'
`

		policyJSON, err := yamlToJSON([]byte(fmt.Sprintf(policyYAML, test.funcName, test.template)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to convert the policy YAML to JSON: %v\n", err)
			panic(err)
		}

		resolvedResult, err := resolver.ResolveTemplate(policyJSON, nil)
		if err != nil {
			t.Fatalf("Failed to process the policy YAML: %v\n", err)
			panic(err)
		}

		policyResolvedJSON := resolvedResult.resolvedJSON
		var policyResolved interface{}
		err = yaml.Unmarshal(policyResolvedJSON, &policyResolved)

		data, ok := policyResolved.(map[string]interface{})["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("Failed to process the policy YAML reading data key: %v\n", err)
			panic(err)
		}

		actualValue, ok := data[test.funcName].(string)
		if !ok {
			t.Fatalf("Failed testing %s: %v\n", test.funcName, err)
		}

		if actualValue != test.expectedResult {
			t.Fatalf("Test %s failed. expected : %v , got : %v", test.funcName, test.expectedResult, actualValue)
		}
	}
}
