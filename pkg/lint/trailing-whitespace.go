package lint

import (
	"strings"
)

var TrailingWhitespaceMetadata = RuleMetadata{
	ID:          "GTUL001",
	Name:        "Trailing Whitespace",
	Description: "Detects lines with trailing whitespace (spaces or tabs)",
	Severity:    "warning",
	Category:    "style",
}

// trailingWhitespace checks each line of the input template string for
// trailing whitespace. If any line contains trailing spaces or tabs, it returns
// an error indicating the line number and content. Otherwise, it returns nil.
func trailingWhitespace(templateStr string) []LinterRuleViolation {
	var violations []LinterRuleViolation

	lines := strings.Split(templateStr, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")

		// Skip empty lines or comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.TrimRight(trimmed, " \t") != trimmed {
			violations = append(violations, LinterRuleViolation{
				LineNumber:    i + 1,
				RuleID:        TrailingWhitespaceMetadata.ID,
				Message:       "trailing whitespace detected",
				FormattedLine: trimmed + "<<<",
			})
		}
	}

	return violations
}
