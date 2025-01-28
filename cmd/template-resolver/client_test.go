package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/stolostron/go-template-utils/v6/cmd/template-resolver/utils"
)

func TestCLI(t *testing.T) {
	entries, err := testfiles.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	noTestsRun := true

	for _, entry := range entries {
		testName, found := strings.CutPrefix(entry.Name(), "test_")
		if !found || !entry.IsDir() {
			continue
		}

		noTestsRun = false

		t.Run(testName, cliTest(testName))
	}

	if noTestsRun {
		t.Fatal("No CLI tests were run")
	}
}

func cliTest(testName string) func(t *testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		inputBytes, err := utils.HandleFile("testdata/test_" + testName + "/input.yaml")
		if err != nil {
			t.Fatal(err)
		}

		expectedBytes, err := utils.HandleFile("testdata/test_" + testName + "/output.yaml")
		if err != nil {
			t.Fatal(err)
		}

		kcPath := ""
		clusterName := ""
		hubNS := ""

		if strings.HasSuffix(testName, "_hub") {
			kcPath = kubeconfigPath
			clusterName = "local-cluster"
			hubNS = "policies"
		}

		objNamespace := "my-obj-namespace"
		objName := "my-obj-name"

		resolvedYAML, err := utils.ProcessTemplate(inputBytes, kcPath, clusterName, hubNS, objNamespace, objName)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(expectedBytes, resolvedYAML) {
			//nolint: forbidigo
			if testing.Verbose() {
				fmt.Println("\nWanted:\n" + string(expectedBytes))
				fmt.Println("\nGot:\n" + string(resolvedYAML))
			}

			unifiedDiff := difflib.UnifiedDiff{
				A:        difflib.SplitLines(string(expectedBytes)),
				FromFile: "expected",
				B:        difflib.SplitLines(string(resolvedYAML)),
				ToFile:   "resolved",
				Context:  5,
			}

			diff, err := difflib.GetUnifiedDiffString(unifiedDiff)
			if err != nil {
				t.Fatal(err)
			}

			t.Fatalf("Mismatch in resolved output; diff:\n%v", diff)
		}
	}
}
