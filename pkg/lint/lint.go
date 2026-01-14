package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type LinterRuleViolation struct {
	LineNumber    int
	RuleName      string
	Message       string
	FormattedLine string
}

// trailingWhitespace checks each line of the input template string for
// trailing whitespace. If any line contains trailing spaces or tabs, it returns
// an error indicating the line number and content. Otherwise, it returns nil.
func trailingWhitespace(templateStr string) []LinterRuleViolation {
	ruleName := "trailingWhitespace"

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
				RuleName:      ruleName,
				Message:       "trailing whitespace detected",
				FormattedLine: trimmed + "<<<",
			})
		}
	}

	return violations
}

// mismatchedDelimiters checks for mismatched delimiters in the template string.
// It returns an error if the delimiters are not all paired.
func mismatchedDelimiters(templateStr string) []LinterRuleViolation {
	ruleName := "mismatchedDelimiters"

	var violations []LinterRuleViolation

	// This regex finds all template delimiters: {{ or {{hub
	delimiterRegEx := regexp.MustCompile(`({{(hub)?-?)|(-?(hub)?}})`)

	type delimiter struct {
		isOpen bool
		isHub  bool
		value  string
		line   int
	}

	var delimiters []delimiter

	lines := strings.Split(templateStr, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines or comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		lineNum := i + 1
		matches := delimiterRegEx.FindAllString(trimmed, -1)

		for _, match := range matches {
			isOpen := strings.HasPrefix(match, "{{")
			isHub := strings.Contains(match, "hub")
			delim := delimiter{
				value:  match,
				isOpen: isOpen,
				isHub:  isHub,
				line:   lineNum,
			}
			delimiters = append(delimiters, delim)
		}
	}

	openDelimiters := []delimiter{}
	openDelimiter := -1

	for _, delimiter := range delimiters {
		switch {
		case delimiter.isOpen:
			openDelimiters = append(openDelimiters, delimiter)
			openDelimiter++

		case len(openDelimiters) == 0 && !delimiter.isOpen:
			violations = append(violations, LinterRuleViolation{
				LineNumber:    delimiter.line,
				RuleName:      ruleName,
				Message:       fmt.Sprintf("unmatched closing delimiter '%s'", delimiter.value),
				FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
			})

		case !delimiter.isOpen:
			matchingOpen := openDelimiters[openDelimiter]
			if matchingOpen.isHub != delimiter.isHub {
				violations = append(violations, LinterRuleViolation{
					LineNumber:    delimiter.line,
					RuleName:      ruleName,
					Message:       "mismatched hub and managed cluster delimiters",
					FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
				})
			}

			openDelimiters = openDelimiters[:openDelimiter]
			openDelimiter--
		}
	}

	for _, delimiter := range openDelimiters {
		violations = append(violations, LinterRuleViolation{
			LineNumber:    delimiter.line,
			RuleName:      ruleName,
			Message:       fmt.Sprintf("unmatched opening delimiter '%s'", delimiter.value),
			FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
		})
	}

	return violations
}

// unquotedTemplateValues checks for unquoted template values in the template
// string. It returns an error if the template values are not single-quoted.
func unquotedTemplateValues(templateStr string) []LinterRuleViolation {
	ruleName := "unquotedTemplateValues"

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
			violations = append(
				violations, LinterRuleViolation{
					LineNumber:    i + 1,
					RuleName:      ruleName,
					Message:       "array item template should be single-quoted",
					FormattedLine: trimmed,
				})

			continue
		}

		// Check for unquoted templated key-value
		if keyValueRe.MatchString(line) && !keyValueQuotedRe.MatchString(line) {
			violations = append(
				violations, LinterRuleViolation{
					LineNumber:    i + 1,
					RuleName:      ruleName,
					Message:       "template value for key should be single-quoted",
					FormattedLine: trimmed,
				})

			continue
		}
	}

	return violations
}

func OutputStringViolations(violations []LinterRuleViolation) string {
	var output strings.Builder
	for _, violation := range violations {
		output.WriteString(fmt.Sprintf("line %d: %s: %s:\n\t%s\n",
			violation.LineNumber, violation.RuleName, violation.Message, violation.FormattedLine))
	}

	return output.String()
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
