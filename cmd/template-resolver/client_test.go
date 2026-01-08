package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/stolostron/go-template-utils/v7/cmd/template-resolver/utils"
	"github.com/stolostron/go-template-utils/v7/pkg/lint"
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

		configBytes, readErr := readFile(filePrefix + "config.yaml")
		if readErr != nil {
			if !os.IsNotExist(readErr) {
				t.Fatal("Failed to read config file:", readErr)
			}
		}

		var tmplResolver utils.TemplateResolver

		tmpDir := t.TempDir()

		if strings.HasSuffix(testName, "_hub") {
			tmplResolver.HubKubeConfigPath = kubeconfigPath
			tmplResolver.ClusterName = "local-cluster"
			tmplResolver.HubNamespace = "policies"
			tmplResolver.SaveHubResources = filepath.Join(tmpDir, "save_hub_resources.yaml")
		}

		tmplResolver.SaveResources = filepath.Join(tmpDir, "save_resources.yaml")
		tmplResolver.ObjNamespace = "my-obj-namespace"
		tmplResolver.ObjName = "my-obj-name"

		// Overwrite configuration with test configuration
		if len(configBytes) > 0 {
			err = yaml.Unmarshal(configBytes, &tmplResolver)
			if err != nil {
				t.Fatal("Failed to parse config file:", err)
			}
		}

		// Append error to linting output if it exists
		var lintingBytes []byte

		if tmplResolver.Lint {
			violations := utils.Lint(string(inputBytes))

			if len(violations) > 0 {
				lintingBytes = []byte("Found linting issues:\n" + lint.OutputStringViolations(violations) + "\n")
			}

			var sarifOut bytes.Buffer

			err = lint.OutputSARIF(violations, "cmd/template-resolver/"+filePrefix+"input.yaml", &sarifOut)
			if err != nil {
				t.Fatal("Failed to make SARIF report: ", err)
			}

			expectedSarif, readErr := readFile(filePrefix + "report.sarif")
			if readErr != nil {
				if !os.IsNotExist(readErr) {
					t.Fatal("Failed to read error file:", readErr)
				}
			}

			compareBytes(t, expectedSarif, sarifOut.Bytes(), "expected", "actual")
		}

		resolvedYAML, err := tmplResolver.ProcessTemplate(inputBytes)
		if err != nil {
			if len(errorBytes) == 0 {
				t.Fatal(err)
			}

			// If an error file is provided, overwrite the
			// expected and resolved with the error contents
			expectedBytes = errorBytes

			errMatch := regexp.MustCompile("template: tmpl:([0-9]+:){1,2} .*")
			resolvedYAML = errMatch.Find([]byte(err.Error()))

			// If nothing is matched, use the entire error
			if len(resolvedYAML) == 0 {
				resolvedYAML = []byte(err.Error())
			}
		}

		// Append output to linting output if it exists
		if len(lintingBytes) > 0 {
			resolvedYAML = append(lintingBytes, resolvedYAML...)
		}

		// Compare the saved resources. Use hubCluster resources
		// if testName ends with "_hub" as well.
		// otherwise test only managedCluster resources.
		if strings.HasSuffix(testName, "_hub") {
			compareSaveResources(t, testName, tmplResolver.SaveHubResources, true)
		}

		compareSaveResources(t, testName, tmplResolver.SaveResources, false)

		// Compare main output (YAML or error)
		compareBytes(t, expectedBytes, resolvedYAML, "expected", "resolved")
	}
}

// compareBytes compares two byte slices and shows a unified diff if they don't match.
func compareBytes(t *testing.T, expected, actual []byte, expectedLabel, actualLabel string) {
	t.Helper()

	if bytes.Equal(expected, actual) {
		return
	}

	//nolint: forbidigo
	if testing.Verbose() {
		fmt.Printf("\n%s:\n%s\n", expectedLabel, string(expected))
		fmt.Printf("\n%s:\n%s\n", actualLabel, string(actual))
	}

	unifiedDiff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(expected)),
		FromFile: expectedLabel,
		B:        difflib.SplitLines(string(actual)),
		ToFile:   actualLabel,
		Context:  5,
	}

	diff, err := difflib.GetUnifiedDiffString(unifiedDiff)
	if err != nil {
		t.Fatal(err)
	}

	t.Fatalf("Mismatch in output; diff:\n%v", diff)
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
