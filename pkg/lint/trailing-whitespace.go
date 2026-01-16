package lint

import (
	"strings"
)

var TrailingWhitespace = LinterRule{
	metadata: RuleMetadata{
		ID:               trailingWhitespaceID,
		Name:             "trailingWhitespace",
		ShortDescription: "Lines should not have trailing whitespace.",
		Level:            "warning",
	},
	runLinter: findTrailingWhitespace,
}

var trailingWhitespaceID = "GTUL001"

// findTrailingWhitespace checks each line of the input template string for
// trailing whitespace. If any line contains trailing spaces or tabs, it returns
// an error indicating the line number and content. Otherwise, it returns nil.
func findTrailingWhitespace(templateStr string) (violations []LinterRuleViolation) {
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
				RuleID:        trailingWhitespaceID,
				ShortMessage:  "trailing whitespace detected",
				Message:       "Trailing whitespace detected.",
				FormattedLine: trimmed + "<<<",
			})
		}
	}

	return violations
}
