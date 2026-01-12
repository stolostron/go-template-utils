package lint

import (
	"regexp"
	"strings"
)

var InvalidVarSyntax = LinterRule{
	metadata: RuleMetadata{
		ID:               invalidVarSyntaxID,
		Name:             "invalidVariableSyntax",
		ShortDescription: "Invalid variable syntax.",
		FullDescription:  "A variable was defined or referenced with invalid syntax.",
		Level:            "error",
	},
	runLinter: findInvalidVarSyntax,
}

const invalidVarSyntaxID = "GTUL005"

// findInvalidVarSyntax checks for variable syntax errors in template strings
func findInvalidVarSyntax(templateStr string) []LinterRuleViolation {
	var violations []LinterRuleViolation

	invalidVariablePatterns := []struct {
		re           *regexp.Regexp
		shortMessage string
		message      string
	}{
		{
			re:           regexp.MustCompile(`\$\w+(?:\.\w+)*\.\$(?:\.\w+)*`),
			shortMessage: "invalid variable reference inside dot operator",
			message:      "Invalid variable reference inside dot operator.",
		},
		// Additional patterns can be added here in the future
	}

	lines := strings.Split(templateStr, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(line, "#") {
			continue
		}

		for _, pattern := range invalidVariablePatterns {
			matches := pattern.re.FindAllStringIndex(line, -1)
			for _, match := range matches {
				violations = append(violations, LinterRuleViolation{
					LineNumber:    i + 1,
					RuleID:        invalidVarSyntaxID,
					ShortMessage:  pattern.shortMessage,
					Message:       pattern.message,
					FormattedLine: trimmed,
					Column:        bytePosToColumn(line, match[1]-1),
				})
			}
		}
	}

	return violations
}
