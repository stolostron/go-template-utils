package lint

import (
	"regexp"
	"strings"
)

var UnquotedTemplateValuesMetadata = RuleMetadata{
	ID:          "GTUL003",
	Name:        "Unquoted Template Values",
	Description: "Enforces single-quote wrapping around template expressions in YAML",
	Severity:    "warning",
	Category:    "best-practice",
}

// unquotedTemplateValues checks for unquoted template values in the template
// string. It returns an error if the template values are not single-quoted.
func unquotedTemplateValues(templateStr string) []LinterRuleViolation {
	var violations []LinterRuleViolation

	lines := strings.Split(templateStr, "\n")

	// Regex to match a line that is an array item with a template, e.g. "- {{ something }}"
	arrayItemRe := regexp.MustCompile(`^\s*-\s*{{.*}}.*$`)
	// Regex to match a line that is an array item with a *quoted* template, e.g. "- '{{ something }}'"
	arrayItemQuotedRe := regexp.MustCompile(`^\s*-\s*'{{.*}}.*'$`)

	// Regex to match a line that is a key with a template value, e.g. "key: {{ something }}"
	keyValueRe := regexp.MustCompile(`^\s*[^:]+:\s*{{.*}}.*$`)
	// Regex to match a line that is a key with a *quoted* template value, e.g. "key: '{{ something }}'"
	keyValueQuotedRe := regexp.MustCompile(`^\s*[^:]+:\s*'{{.*}}.*'$`)

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
					RuleID:        UnquotedTemplateValuesMetadata.ID,
					Message:       "array item template should be single-quoted",
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
					RuleID:        UnquotedTemplateValuesMetadata.ID,
					Message:       "template value for key should be single-quoted",
					FormattedLine: trimmed,
					Column:        bytePosToColumn(line, templateStart),
				})

			continue
		}
	}

	return violations
}
