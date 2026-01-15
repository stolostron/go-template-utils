package lint

import (
	"regexp"
	"strings"
)

var MismatchedQuotes = LinterRule{
	metadata: RuleMetadata{
		ID:               mismatchedQuotesID,
		Name:             "mismatchedQuotes",
		ShortDescription: "Mismatched quotes should be removed or paired with a closing quote.",
		FullDescription:  "Mismatched quotes should be removed or paired with a closing quote. ",
		Level:            "warning",
	},
	runLinter: findMismatchedQuotes,
}

const mismatchedQuotesID = "GTUL006"

func findMismatchedQuotes(templateStr string) []LinterRuleViolation {
	var violations []LinterRuleViolation
	const shortMessage = "unmatched or invalid quote detected"
	const message = "Unmatched or invalid quote detected."

	lines := strings.Split(templateStr, "\n")

	keyPattern := `^(\s*)\w[-\w./_]*\w\s*:\s*`
	// Has an unescaped double quote in a double quoted string
	hasBadDoubleQuoteRe := regexp.MustCompile(keyPattern + `"(?:[^"\\]|\\.)*"(?:(?:[^"\\]|\\.)*")*"$`)
	// Has an unescaped single quote in a single quoted string
	hasBadSingleQuoteStrRe := regexp.MustCompile(keyPattern + `'(?:'[^']*|[^'{}]*')'$`)

	// Track quote counts within template strings
	singleQuoteCount := 0
	doubleQuoteCount := 0
	inTemplateString := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !inTemplateString {
			singleQuoteCount = 0
			doubleQuoteCount = 0
		}

		// Count quotes within a template string
		inDoubleQuoteStr := false
		prevSingleQuoteCount := singleQuoteCount
		prevDoubleQuoteCount := doubleQuoteCount
		singleQuoteViolation := false
		doubleQuoteViolation := false

		for j, char := range trimmed {
			singleQuoteViolation = false
			doubleQuoteViolation = false

			// Look ahead two characters for {{ or }}
			if j+1 < len(trimmed) {
				nextChar := rune(trimmed[j+1])
				if char == '{' && nextChar == '{' {
					inTemplateString = true
				}

				if char == '}' && nextChar == '}' {
					inTemplateString = false
					// Mismatched quote appeared when the count changed from even to odd
					singleQuoteViolation = singleQuoteCount%2 == 1 && prevSingleQuoteCount%2 == 0
					doubleQuoteViolation = doubleQuoteCount%2 == 1 && prevDoubleQuoteCount%2 == 0
				}
			}

			if singleQuoteViolation || doubleQuoteViolation {
				break
			}

			if !inTemplateString {
				continue
			}

			switch char {
			case '\'':
				if !inDoubleQuoteStr {
					singleQuoteCount++
				}
			case '"':
				doubleQuoteCount++
				inDoubleQuoteStr = !inDoubleQuoteStr
			}
		}

		// Detect even pairs of invalid quotes inside regular string values
		if hasBadDoubleQuoteRe.MatchString(trimmed) {
			doubleQuoteViolation = true
		}

		if hasBadSingleQuoteStrRe.MatchString(trimmed) {
			singleQuoteViolation = true
		}

		if singleQuoteViolation || doubleQuoteViolation {
			violations = append(violations, LinterRuleViolation{
				LineNumber:    i + 1,
				RuleID:        mismatchedQuotesID,
				ShortMessage:  shortMessage,
				Message:       message,
				FormattedLine: trimmed,
			})
		}
	}

	return violations
}
