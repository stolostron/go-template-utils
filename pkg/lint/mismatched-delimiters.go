package lint

import (
	"fmt"
	"regexp"
	"strings"
)

var MismatchedDelimiters = LinterRule{
	metadata: RuleMetadata{
		ID:               mismatchedDelimitersID,
		Name:             "mismatchedDelimiters",
		ShortDescription: "Template delimiters must be properly paired.",
		FullDescription: "Template start delimiters (`{{` or `{{hub`) must be paired with a corresponding " +
			"closing delimiter (`}}` or `hub}}`). Otherwise, the template is invalid.",
		Level: "error",
	},
	runLinter: findMismatchedDelimiters,
}

const mismatchedDelimitersID = "GTUL002"

// findMismatchedDelimiters checks for mismatched delimiters in the template string.
// It returns an error if the delimiters are not all paired.
func findMismatchedDelimiters(templateStr string) (violations []LinterRuleViolation) {
	// This regex finds all template delimiters: {{ or {{hub
	delimiterRegEx := regexp.MustCompile(`({{(hub)?-?)|(-?(hub)?}})`)

	type delimiter struct {
		isOpen bool
		isHub  bool
		value  string
		line   int
		column int
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
		matches := delimiterRegEx.FindAllStringIndex(trimmed, -1)

		// Find how many leading characters were trimmed from the original line
		leadingWhitespaceLen := len(line) - len(strings.TrimLeft(line, " \t"))

		for _, match := range matches {
			bytePos := match[0]
			matchStr := trimmed[bytePos:match[1]]
			isOpen := strings.HasPrefix(matchStr, "{{")
			isHub := strings.Contains(matchStr, "hub")
			// Calculate column in the original line by mapping the position from trimmed to line
			// The delimiter is at bytePos in trimmed, which maps to leadingWhitespaceLen + bytePos in line
			column := bytePosToColumn(line, leadingWhitespaceLen+bytePos)
			delim := delimiter{
				value:  matchStr,
				isOpen: isOpen,
				isHub:  isHub,
				line:   lineNum,
				column: column,
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
				RuleID:        mismatchedDelimitersID,
				ShortMessage:  fmt.Sprintf("unmatched closing delimiter '%s'", delimiter.value),
				Message:       fmt.Sprintf("Unmatched closing delimiter '%s'.", delimiter.value),
				FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
				Column:        delimiter.column,
			})

		case !delimiter.isOpen:
			matchingOpen := openDelimiters[openDelimiter]
			if matchingOpen.isHub != delimiter.isHub {
				violations = append(violations, LinterRuleViolation{
					LineNumber:    delimiter.line,
					RuleID:        mismatchedDelimitersID,
					ShortMessage:  "mismatched hub and managed cluster delimiters",
					Message:       "Mismatched hub and managed cluster delimiters.",
					FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
					Column:        delimiter.column,
				})
			}

			openDelimiters = openDelimiters[:openDelimiter]
			openDelimiter--
		}
	}

	for _, delimiter := range openDelimiters {
		violations = append(violations, LinterRuleViolation{
			LineNumber:    delimiter.line,
			RuleID:        mismatchedDelimitersID,
			ShortMessage:  fmt.Sprintf("unmatched opening delimiter '%s'", delimiter.value),
			Message:       fmt.Sprintf("Unmatched opening delimiter '%s'.", delimiter.value),
			FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
			Column:        delimiter.column,
		})
	}

	return violations
}
