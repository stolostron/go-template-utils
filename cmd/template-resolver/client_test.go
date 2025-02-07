package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		saveHubResources := ""

		tmpDir := t.TempDir()

		if strings.HasSuffix(testName, "_hub") {
			kcPath = kubeconfigPath
			clusterName = "local-cluster"
			hubNS = "policies"
			saveHubResources = filepath.Join(tmpDir, "save_hub_resources.yaml")
		}

		objNamespace := "my-obj-namespace"
		objName := "my-obj-name"

		saveResources := filepath.Join(tmpDir, "save_resources.yaml")

		resolvedYAML, err := utils.ProcessTemplate(inputBytes, kcPath, clusterName,
			hubNS, objNamespace, objName, saveResources, saveHubResources)
		if err != nil {
			t.Fatal(err)
		}

		// Compare the saved resources. Use hubCluster resources
		// if testName ends with "_hub" as well.
		// otherwise test only managedCluster resources.
		if strings.HasSuffix(testName, "_hub") {
			compareSaveResources(t, testName, saveHubResources, true)
		}

		compareSaveResources(t, testName, saveResources, false)

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

func compareSaveResources(t *testing.T, testName, saveResources string, isHub bool) {
	t.Helper()

	fileName := "save_resources.yaml"
	if isHub {
		fileName = "save_hub_resources.yaml"
	}

	expectedPath := fmt.Sprintf("testdata/test_%s/%s", testName, fileName)

	expectedBytes, err := readFile(expectedPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("Expected file is missing, skipping %s comparison", fileName)

			return
		}

		t.Fatal("Failed to read expected file:", err)
	}

	actualBytes, err := readFile(saveResources)
	if err != nil {
		t.Fatal("Failed to read actual file:", err)
	}

	if len(actualBytes) == 0 {
		t.Fatalf("Actual file %s is empty, expected file has content", testName+"/"+fileName)
	}

	// Compare resource counts
	expectCount := strings.Count(string(expectedBytes), "---")
	actualCount := strings.Count(string(actualBytes), "---")

	if expectCount != actualCount {
		t.Fatalf("Mismatch in resource count: expected %d, but got %d", expectCount, actualCount)
	}

	// Validate expected lines exist in actual output.
	// To ignore field like creationTimestamp, resourceVersion, uid
	expectScanner := bufio.NewScanner(bytes.NewBuffer(expectedBytes))
	for expectScanner.Scan() {
		expectLine := expectScanner.Text()
		if expectLine != "---" && !strings.Contains(string(actualBytes), expectLine) {
			t.Fatalf("The line '%s' is missing in actual result", expectLine)
		}
	}

	if err := expectScanner.Err(); err != nil {
		t.Fatalf("Error reading expected file: %v", err)
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
