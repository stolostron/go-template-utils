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
		FullDescription: "Template and JSON start delimiters (`{{` or `{{hub` or `{`) " +
			"must be paired with a corresponding closing delimiter. Otherwise, the template is invalid.",
		Level: "error",
	},
	runLinter: findMismatchedDelimiters,
}

const mismatchedDelimitersID = "GTUL002"

// findMismatchedDelimiters checks for mismatched delimiters in the template string.
// It returns an error if the delimiters are not all paired.
func findMismatchedDelimiters(templateStr string) (violations []LinterRuleViolation) {
	// This regex finds all template or JSON delimiters: { or {{ or {{hub
	delimiterRegEx := regexp.MustCompile(`({{(hub)?-?)|(-?(hub)?}}|[{}])`)

	type delimiter struct {
		isOpen bool
		isHub  bool
		isJSON bool
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
			isOpen := strings.HasPrefix(matchStr, "{")
			isHub := strings.Contains(matchStr, "hub")
			isJSON := matchStr == "{" || matchStr == "}"

			// Calculate column in the original line by mapping the position from trimmed to line
			// The delimiter is at bytePos in trimmed, which maps to leadingWhitespaceLen + bytePos in line
			column := bytePosToColumn(line, leadingWhitespaceLen+bytePos)
			delim := delimiter{
				value:  matchStr,
				isOpen: isOpen,
				isHub:  isHub,
				isJSON: isJSON,
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
		// If it's an opening delimiter, add it to the list of open delimiters
		case delimiter.isOpen:
			openDelimiters = append(openDelimiters, delimiter)
			openDelimiter++

		// If it's a closing delimiter and there are no open delimiters, add a violation
		case len(openDelimiters) == 0 && !delimiter.isOpen:
			violations = append(violations, LinterRuleViolation{
				LineNumber:    delimiter.line,
				RuleID:        mismatchedDelimitersID,
				ShortMessage:  fmt.Sprintf("unmatched closing delimiter '%s'", delimiter.value),
				Message:       fmt.Sprintf("Unmatched closing delimiter '%s'.", delimiter.value),
				FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
				Column:        delimiter.column,
			})

		// If it's a closing delimiter and there are open delimiters,
		// check if it matches the last open delimiter
		case !delimiter.isOpen:
			matchingOpen := openDelimiters[openDelimiter]

			// Handle when it matches a template delimiter
			// but it's actually paired with a JSON delimiter
			if delimiter.value == "}}" && matchingOpen.isJSON {
				openDelimiters = openDelimiters[:openDelimiter]
				openDelimiter--

				// After consuming the last open delimiter, if there are no more open
				// delimiters, add a violation for the unmatched closing JSON delimiter
				if len(openDelimiters) == 0 {
					violations = append(violations, LinterRuleViolation{
						LineNumber:    delimiter.line,
						RuleID:        mismatchedDelimitersID,
						ShortMessage:  "unmatched closing JSON delimiter '}'",
						Message:       "Unmatched closing JSON delimiter '}'.",
						FormattedLine: strings.TrimSpace(lines[delimiter.line-1]),
						Column:        delimiter.column,
					})

					continue
				}

				// Fetch the next open delimiter and check if it's also a JSON delimiter
				matchingOpen = openDelimiters[openDelimiter]
				if matchingOpen.isJSON {
					openDelimiters = openDelimiters[:openDelimiter]
					openDelimiter--

					continue
				}

				// If the next open delimiter is not a JSON delimiter,
				// update the current delimiter to be a JSON delimiter
				delimiter.value = "}"
				delimiter.isJSON = true
				delimiter.column++
			}

			var line string
			var lineNum, column int

			// Prefer showing the line of the opening delimiter
			if matchingOpen.line == delimiter.line {
				lineNum = matchingOpen.line
				line = strings.TrimSpace(lines[delimiter.line-1])
				column = delimiter.column
			} else {
				lineNum = matchingOpen.line
				line = strings.TrimSpace(lines[matchingOpen.line-1])
				column = matchingOpen.column
			}

			// Handle when the opening and closing delimiters are mismatched
			if matchingOpen.isHub != delimiter.isHub && !delimiter.isJSON {
				violations = append(violations, LinterRuleViolation{
					LineNumber:   lineNum,
					RuleID:       mismatchedDelimitersID,
					ShortMessage: "mismatched hub and managed cluster delimiters",
					Message: "Mismatched hub and managed cluster delimiters: " +
						matchingOpen.value + " ... " + delimiter.value,
					FormattedLine: line,
					Column:        column,
				})
			} else if matchingOpen.isJSON != delimiter.isJSON {
				violations = append(violations, LinterRuleViolation{
					LineNumber:   lineNum,
					RuleID:       mismatchedDelimitersID,
					ShortMessage: "mismatched JSON and template delimiters",
					Message: "Mismatched JSON and template delimiters: " +
						matchingOpen.value + " ... " + delimiter.value,
					FormattedLine: line,
					Column:        column,
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
