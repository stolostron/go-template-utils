package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/stolostron/go-template-utils/v7/pkg/lint/sarif"
)

type LinterRule struct {
	metadata  RuleMetadata
	runLinter func(string) []LinterRuleViolation
}

var linterRules = []LinterRule{
	TrailingWhitespace,
	MismatchedDelimiters,
	UnquotedTemplateValues,
	InvalidVarSyntax,
}

// LinterRuleViolation represents a single violation of a linting rule.
// It contains information about where the violation occurred and what the issue is.
type LinterRuleViolation struct {
	// LineNumber is the 1-based line number where the violation occurred
	LineNumber int

	// RuleID is the unique identifier for the rule that was violated (e.g., "GTUL001")
	RuleID string

	// ShortMessage provides a quick description of the violation, in one uncapitalized clause
	ShortMessage string

	// Message provides specific information about the violation, in full sentences
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

	// Name is a human-readable name for the rule, in camelCase format.
	Name string

	// ShortDescription explains the rule in one sentence.
	ShortDescription string

	// FullDescription describes the rule with additional context, and suggestions
	// for resolving violations of the rule, or instances where it can be ignored.
	// The FullDescription can use GH-flavored markdown for formatting.
	FullDescription string

	// Level indicates the severity of the violation: "error", "warning", or "note"
	Level string
}

// getRuleMetadata returns the RuleMetadata for a given rule ID, or nil if not found.
func getRuleMetadata(ruleID string) *RuleMetadata {
	for _, rule := range linterRules {
		if rule.metadata.ID == ruleID {
			return &rule.metadata
		}
	}

	return nil
}

func OutputStringViolations(violations []LinterRuleViolation) string {
	var output strings.Builder

	for _, violation := range violations {
		ruleMD := getRuleMetadata(violation.RuleID)
		if ruleMD == nil {
			output.WriteString(fmt.Sprintf("line %d: unknown rule: %s: %s:\n\t%s\n",
				violation.LineNumber, violation.RuleID, violation.ShortMessage, violation.FormattedLine))

			continue
		}

		output.WriteString(fmt.Sprintf("line %d: %s: %s:\n\t%s\n",
			violation.LineNumber, ruleMD.Name, violation.ShortMessage, violation.FormattedLine))
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
			rule := sarif.NewRule(metadata.ID, metadata.Name, metadata.ShortDescription)

			if metadata.FullDescription != "" {
				rule.FullDescription = &sarif.Message{
					Text:     metadata.FullDescription,
					Markdown: metadata.FullDescription,
				}
			}

			rules = append(rules, rule)

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
		res := sarif.NewResult(metadata.Level, violation.Message, violation.RuleID,
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
func Lint(templateStr string) (violations []LinterRuleViolation) {
	for _, rule := range linterRules {
		violations = append(violations, rule.runLinter(templateStr)...)
	}

	if len(violations) > 0 {
		sort.Slice(violations, func(i, j int) bool {
			return violations[i].LineNumber < violations[j].LineNumber
		})

		return violations
	}

	return nil
}
