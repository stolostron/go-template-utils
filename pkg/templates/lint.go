package templates

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

// unusedVariables checks for variables that are defined but not used within the
// same template scope
func unusedVariables(templateStr string) []LinterRuleViolation {
	const newlineSep = "\n"
	var violations []LinterRuleViolation

	lines := strings.Split(templateStr, newlineSep)
	lenTmplStr := len(templateStr)

	isRaw := false
	rawModePattern := regexp.MustCompile(`^\s*object-templates-raw\s*:`)

	for _, line := range lines {
		if rawModePattern.MatchString(line) {
			isRaw = true

			break
		}
	}

	type templateWithLine struct {
		template string
		lineNum  int
	}

	extractTemplatesFromLines := func(
		lines []string, lineOffset int,
	) (hubTemplates, managedTemplates []templateWithLine) {
		hubTmplRe := regexp.MustCompile(`{{hub\s+.*?\s+hub}}`)
		tmplRe := regexp.MustCompile(`{{-?.*?-?}}`)

		for i, line := range lines {
			lineNum := lineOffset + i + 1

			allMatches := tmplRe.FindAllString(line, -1)
			for _, match := range allMatches {
				if hubTmplRe.MatchString(match) {
					hubTemplates = append(hubTemplates, templateWithLine{
						template: match,
						lineNum:  lineNum,
					})
				} else {
					managedTemplates = append(managedTemplates, templateWithLine{
						template: match,
						lineNum:  lineNum,
					})
				}
			}
		}

		return hubTemplates, managedTemplates
	}

	isPositionInRanges := func(pos int, ranges [][]int) bool {
		for _, r := range ranges {
			if pos >= r[0] && pos < r[1] {
				return true
			}
		}

		return false
	}

	// Check for unused variables in a single hub or managed scope
	checkUnusedVars := func(templates []templateWithLine) {
		varRe := regexp.MustCompile(`\$(\w+)`)
		stringLiteralRe := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
		commentRe := regexp.MustCompile(`{{-?\s*/\*.*?\*/\s*-?}}`)

		if len(templates) == 0 {
			return
		}

		type varDefinition struct {
			name     string
			position int
			lineNum  int
			used     bool
		}

		definitionsByName := make(map[string][]*varDefinition)

		globalPos := 0 // Track position across all templates

		for _, tmpl := range templates {
			templateStr := tmpl.template
			lineNum := tmpl.lineNum

			varMatches := varRe.FindAllStringSubmatchIndex(templateStr, -1)
			stringRanges := stringLiteralRe.FindAllStringIndex(templateStr, -1)
			commentRanges := commentRe.FindAllStringIndex(templateStr, -1)

			for _, match := range varMatches {
				localPos := match[0]
				varNameStart := match[2]
				varNameEnd := match[3]

				// Skip if inside string literal or comment
				if isPositionInRanges(localPos, stringRanges) ||
					isPositionInRanges(localPos, commentRanges) {
					continue
				}

				varName := templateStr[varNameStart:varNameEnd]

				if varName == "_" {
					continue
				}

				// Check if this is a definition by looking at what follows the variable name
				isDefinition := false
				afterVar := varNameEnd

				if afterVar < lenTmplStr {
					// Skip whitespace
					for afterVar < lenTmplStr && (templateStr[afterVar] == ' ' || templateStr[afterVar] == '\t') {
						afterVar++
					}

					// Check for := or ,
					if afterVar < lenTmplStr {
						if templateStr[afterVar] == ',' {
							isDefinition = true
						} else if afterVar+1 < lenTmplStr && templateStr[afterVar:afterVar+2] == ":=" {
							isDefinition = true
						}
					}
				}

				globalVarPos := globalPos + localPos

				if isDefinition {
					// Store the definition
					def := &varDefinition{
						name:     varName,
						position: globalVarPos,
						lineNum:  lineNum,
						used:     false,
					}
					definitionsByName[varName] = append(definitionsByName[varName], def)
				} else {
					// Mark the most recent definition as used
					defs := definitionsByName[varName]
					for i := len(defs) - 1; i >= 0; i-- {
						if defs[i].position < globalVarPos {
							defs[i].used = true

							break
						}
					}
				}
			}

			globalPos += lenTmplStr
		}

		// Report unused definitions
		for _, defs := range definitionsByName {
			for _, def := range defs {
				if !def.used {
					violations = append(violations, LinterRuleViolation{
						LineNumber:    def.lineNum,
						RuleName:      "unusedVariables",
						Message:       fmt.Sprintf("variable '$%s' is defined but not used within scope", def.name),
						FormattedLine: strings.TrimSpace(lines[def.lineNum-1]),
					})
				}
			}
		}
	}

	// Determine block boundaries by line number
	var blockStartLines []int
	if isRaw {
		// Object templates raw: entire template is one block
		blockStartLines = []int{0}
	} else {
		// Object definition(s): find lines that start a new objectDefinition
		blockStartPattern := regexp.MustCompile(`^\s+objectDefinition:`)

		for i, line := range lines {
			if blockStartPattern.MatchString(line) {
				blockStartLines = append(blockStartLines, i)
			}
		}

		if len(blockStartLines) == 0 {
			return violations
		}
	}

	for i, startLine := range blockStartLines {
		endLine := len(lines)
		if i+1 < len(blockStartLines) {
			endLine = blockStartLines[i+1]
		}

		blockLines := lines[startLine:endLine]
		lineOffset := startLine

		hubTemplates, managedTemplates := extractTemplatesFromLines(blockLines, lineOffset)
		checkUnusedVars(hubTemplates)
		checkUnusedVars(managedTemplates)
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
		unusedVariables,
	}

	for _, check := range lintingChecks {
		violations = append(violations, check(templateStr)...)
	}

	if len(violations) > 0 {
		sort.Slice(violations, func(i, j int) bool {
			if violations[i].LineNumber != violations[j].LineNumber {
				return violations[i].LineNumber < violations[j].LineNumber
			}
			// For violations on the same line, sort by message for deterministic ordering
			return violations[i].Message < violations[j].Message
		})

		return violations
	}

	return nil
}
