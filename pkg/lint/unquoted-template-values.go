package lint

import (
	"regexp"
	"strings"
)

var UnquotedTemplateValues = LinterRule{
	metadata: RuleMetadata{
		ID:               unquotedTemplateValuesID,
		Name:             "unquotedTemplateValues",
		ShortDescription: "Template expressions should be single-quoted.",
		FullDescription: "Single-quote wrapping around template expressions in YAML will ensure that the " +
			"templates are properly interpreted and not interfere with the rest of the structure.",
		Level: "warning",
	},
	runLinter: findUnquotedTemplateValues,
}

const unquotedTemplateValuesID = "GTUL003"

// findUnquotedTemplateValues checks for unquoted template values in the template
// string. It returns an error if the template values are not single-quoted.
func findUnquotedTemplateValues(templateStr string) (violations []LinterRuleViolation) {
	lines := strings.Split(templateStr, "\n")

	// Regex to match a line that is an array item with a template, e.g. "- {{ something }}"
	arrayItemRe := regexp.MustCompile(`^\s*-\s*{{.*}}.*$`)
	// Regex to match a line that is an array item with a *quoted* template, e.g. "- '{{ something }}'"
	arrayItemQuotedRe := regexp.MustCompile(`^\s*-\s*'{{.*}}.*'$`)

	// Regex to match a line that is a key with a template value, e.g. "key: {{ something }}"
	keyValueRe := regexp.MustCompile(`^\s*[^:]+:\s*{{.*}}.*$`)
	// Regex to match a line that is a key with a *quoted* template value, e.g. "key: '{{ something }}'"
	keyValueQuotedRe := regexp.MustCompile(`^\s*[^:]+:\s*'{{.*}}.*'$`)

	// Regex to match }}'"
	extraSingleQuoteAfterCloseRe := regexp.MustCompile(`}}'"\s*$`)
	// Regex to match }}''
	extraDoubleQuoteAfterCloseRe := regexp.MustCompile(`}}''\s*$`)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines or comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check for unquoted templated array value
		if arrayItemRe.MatchString(line) && !arrayItemQuotedRe.MatchString(line) {
			// Find where {{ starts in the line
			templateStart := strings.Index(line, "{{")

			violations = append(
				violations, LinterRuleViolation{
					LineNumber:    i + 1,
					RuleID:        unquotedTemplateValuesID,
					ShortMessage:  "templates for array items should be single-quoted",
					Message:       "Templates for array items should be single-quoted.",
					FormattedLine: trimmed,
					Column:        bytePosToColumn(line, templateStart),
				})

			continue
		}

		// Check for unquoted templated key-value
		if keyValueRe.MatchString(line) && !keyValueQuotedRe.MatchString(line) {
			// Find where {{ starts in the line
			templateStart := strings.Index(line, "{{")

			violations = append(
				violations, LinterRuleViolation{
					LineNumber:    i + 1,
					RuleID:        unquotedTemplateValuesID,
					ShortMessage:  "templates should be single-quoted",
					Message:       "Templates should be single-quoted.",
					FormattedLine: trimmed,
					Column:        bytePosToColumn(line, templateStart),
				})

			continue
		}

		// Check for extra quotes after closing delimiters
		if extraSingleQuoteAfterCloseRe.MatchString(line) || extraDoubleQuoteAfterCloseRe.MatchString(line) {
			templateEnd := strings.Index(line, "}}")
			violations = append(violations, LinterRuleViolation{
				LineNumber:    i + 1,
				RuleID:        unquotedTemplateValuesID,
				ShortMessage:  "extra quote after closing template delimiter",
				Message:       "Extra quote after closing template delimiter.",
				FormattedLine: trimmed,
				Column:        bytePosToColumn(line, templateEnd),
			})
		}
	}

	return violations
}
