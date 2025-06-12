package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
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

		filePrefix := "testdata/test_" + testName + "/"

		inputBytes, err := utils.HandleFile(filePrefix + "input.yaml")
		if err != nil {
			t.Fatal(err)
		}

		expectedBytes := []byte{}

		errorBytes, readErr := readFile(filePrefix + "error.txt")
		if readErr != nil {
			if !os.IsNotExist(readErr) {
				t.Fatal("Failed to read error file:", readErr)
			}
		}

		if len(errorBytes) == 0 {
			expectedBytes, err = utils.HandleFile(filePrefix + "output.yaml")
			if err != nil {
				t.Fatal(err)
			}
		}

		kcPath := ""
		clusterName := ""
		hubNS := ""

		if strings.HasSuffix(testName, "_hub") {
			kcPath = kubeconfigPath
			clusterName = "local-cluster"
			hubNS = "policies"
		}

		resolvedYAML, err := utils.ProcessTemplate(inputBytes, kcPath, clusterName, hubNS)
		if err != nil {
			if len(errorBytes) == 0 {
				t.Fatal(err)
			}

			// If an error file is provided, overwrite the
			// expected and resolved with the error contents
			expectedBytes = errorBytes
			errMatch := regexp.MustCompile("template: tmpl:[0-9]+:[0-9]+: .*")
			resolvedYAML = errMatch.Find([]byte(err.Error()))
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

// readFile is a helper function to read file contents.
func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	return io.ReadAll(f)
}
