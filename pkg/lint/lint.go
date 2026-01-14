package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/stolostron/go-template-utils/v7/pkg/lint/sarif"
)

// LinterRuleViolation represents a single violation of a linting rule.
// It contains information about where the violation occurred and what the issue is.
type LinterRuleViolation struct {
	// LineNumber is the 1-based line number where the violation occurred
	LineNumber int

	// RuleID is the unique identifier for the rule that was violated (e.g., "GTUL001")
	RuleID string

	// Message describes the specific violation that was detected
	Message string

	// FormattedLine is the line content where the violation occurred, formatted for display
	FormattedLine string

	// Column is the 1-based column number where the violation occurred.
	// If 0, the column information is not available or not applicable.
	Column int
}

// RuleMetadata contains metadata about a linting rule.
// This includes information such as a rule ID, a human-readable name,
// a description, help text, severity level, and category.
type RuleMetadata struct {
	// ID is a unique identifier for the rule, in the format "GTUL###"
	// This is used as the ruleId in SARIF output.
	ID string

	// Name is a human-readable name for the rule
	Name string

	// Description explains what the rule checks for
	Description string

	// Severity indicates the severity level of violations: "error", "warning", or "note"
	Severity string

	// Category groups related rules together, e.g., "style", "syntax", "best-practice"
	Category string
}

// getRuleMetadata returns the RuleMetadata for a given rule ID, or nil if not found.
func getRuleMetadata(ruleID string) *RuleMetadata {
	switch ruleID {
	case TrailingWhitespaceMetadata.ID:
		return &TrailingWhitespaceMetadata
	case MismatchedDelimitersMetadata.ID:
		return &MismatchedDelimitersMetadata
	case UnquotedTemplateValuesMetadata.ID:
		return &UnquotedTemplateValuesMetadata
	default:
		return nil
	}
}

func OutputStringViolations(violations []LinterRuleViolation) string {
	var output strings.Builder

	for _, violation := range violations {
		ruleMD := getRuleMetadata(violation.RuleID)
		if ruleMD == nil {
			output.WriteString(fmt.Sprintf("line %d: unknown rule: %s: %s:\n\t%s\n",
				violation.LineNumber, violation.RuleID, violation.Message, violation.FormattedLine))

			continue
		}

		output.WriteString(fmt.Sprintf("line %d: %s: %s:\n\t%s\n",
			violation.LineNumber, ruleMD.Name, violation.Message, violation.FormattedLine))
	}

	return output.String()
}

// OutputSARIF writes a SARIF report to the output, given a list of linter violations.
// The inputFile parameter specifies the file path/URI that was linted.
func OutputSARIF(violations []LinterRuleViolation, inputFile string, output io.Writer) error {
	// We will only put the rules that were actually violated in the SARIF report.
	usedRuleIDs := make(map[string]bool)
	usedRuleIDsList := make([]string, 0, len(usedRuleIDs))

	for _, violation := range violations {
		if !usedRuleIDs[violation.RuleID] {
			usedRuleIDsList = append(usedRuleIDsList, violation.RuleID)

			usedRuleIDs[violation.RuleID] = true
		}
	}

	sort.Strings(usedRuleIDsList)

	// Create a map of ruleID to rule index, and prepare those rules for the report
	ruleIndexMap := make(map[string]int)
	rules := make([]sarif.Rule, 0, len(usedRuleIDsList))

	for i, ruleID := range usedRuleIDsList {
		if metadata := getRuleMetadata(ruleID); metadata != nil {
			rules = append(rules, sarif.NewRule(metadata.ID, metadata.Name, metadata.Description))

			ruleIndexMap[ruleID] = i
		} else {
			return fmt.Errorf("unknown rule ID: %v", ruleID)
		}
	}

	// Process each violation: build the results with proper indices
	results := make([]sarif.Result, 0, len(violations))

	for _, violation := range violations {
		metadata := getRuleMetadata(violation.RuleID)
		if metadata == nil {
			return fmt.Errorf("unknown rule ID: %v", violation.RuleID)
		}

		loc := sarif.NewLocation(inputFile, 0, violation.LineNumber, violation.Column)
		res := sarif.NewResult(metadata.Severity, violation.Message, violation.RuleID,
			ruleIndexMap[violation.RuleID], loc)

		results = append(results, res)
	}

	run := sarif.NewRun("go-template-utils-linter", "https://github.com/stolostron/go-template-utils").
		WithRules(rules...).
		WithArtifacts(sarif.NewArtifact(inputFile)).
		WithResults(results...)

	enc := json.NewEncoder(output)

	enc.SetIndent("", "  ")

	return enc.Encode(sarif.NewReport(run))
}

// lint checks the template string for linting errors.
func Lint(templateStr string) []LinterRuleViolation {
	var violations []LinterRuleViolation

	lintingChecks := []func(string) []LinterRuleViolation{
		trailingWhitespace,
		mismatchedDelimiters,
		unquotedTemplateValues,
	}

	for _, check := range lintingChecks {
		violations = append(violations, check(templateStr)...)
	}

	if len(violations) > 0 {
		sort.Slice(violations, func(i, j int) bool {
			return violations[i].LineNumber < violations[j].LineNumber
		})

		return violations
	}

	return nil
}
