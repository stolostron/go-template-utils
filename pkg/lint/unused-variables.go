package lint

import (
	"fmt"
	"regexp"
	"strings"
)

var UnusedVariables = LinterRule{
	metadata: RuleMetadata{
		ID:               unusedVariablesID,
		Name:             "unusedVariables",
		ShortDescription: "Unused variables should be removed or used in the template.",
		FullDescription: "Unused variables should be removed or used in the template. " +
			"The variable definition may be unused within the variable's scope or " +
			"overwritten before use.",
		Level: "warning",
	},
	runLinter: findUnusedVariables,
}

const unusedVariablesID = "GTUL004"

// findUnusedVariables checks for variables that are defined but not used within the
// same template scope
func findUnusedVariables(templateStr string) []LinterRuleViolation {
	var violations []LinterRuleViolation

	type templateWithLine struct {
		template string
		lineNum  int
	}

	varRe := regexp.MustCompile(`\$(\w+)(?:\.\w+)*`)
	varDefRe := regexp.MustCompile(`\$(\w+)\s*[:=,]`)
	hubTmplRe := regexp.MustCompile(`{{hub\s+.*?\s+hub}}`)
	tmplRe := regexp.MustCompile(`{{-?.*?-?}}`)
	rawTmplRe := regexp.MustCompile(`(?m)^\s*object-templates-raw\s*:`)
	isRaw := rawTmplRe.MatchString(templateStr)

	// Prevent false matches by replacing the content of
	// string literals and comments with spaces
	stringLiteralRe := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	commentRe := regexp.MustCompile(`{{-?\s*/\*.*?\*/\s*-?}}`)

	toSpaces := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return '\n'
			}

			return ' '
		}, s)
	}

	cleanedStr := stringLiteralRe.ReplaceAllStringFunc(templateStr, toSpaces)
	cleanedStr = commentRe.ReplaceAllStringFunc(cleanedStr, toSpaces)
	cleanedLines := strings.Split(cleanedStr, "\n")

	lines := strings.Split(templateStr, "\n")

	extractTmplsFromLines := func(
		lines []string, lineOffset int,
	) (hubTemplates, managedTemplates []templateWithLine) {
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)

			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}

			lineNum := lineOffset + i + 1
			allMatches := tmplRe.FindAllString(trimmed, -1)

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

	// Check for unused variables in a single hub or managed scope
	checkUnusedVars := func(templates []templateWithLine) {
		if len(templates) == 0 {
			return
		}

		type varDefinition struct {
			lineNum int
			used    bool
			bytePos int
		}

		definitionsByName := make(map[string][]*varDefinition)
		isDef := make(map[int]bool)

		globalPos := 0

		for _, tmpl := range templates {
			line := tmpl.template
			lineNum := tmpl.lineNum

			varDefMatches := varDefRe.FindAllStringSubmatchIndex(line, -1)
			// Track variable definitions by position
			for _, defMatch := range varDefMatches {
				isDef[globalPos+defMatch[0]] = true
			}

			varMatches := varRe.FindAllStringSubmatchIndex(line, -1)

			for _, match := range varMatches {
				localPos := match[0]
				varName := line[match[2]:match[3]]

				if varName == "_" {
					continue
				}

				if isDef[globalPos+localPos] {
					def := &varDefinition{
						lineNum: lineNum,
						used:    false,
						bytePos: localPos,
					}
					// Store the definition for future references
					definitionsByName[varName] = append(definitionsByName[varName], def)
				} else {
					// Mark the most recent definition as used
					defs := definitionsByName[varName]
					if len(defs) > 0 {
						defs[len(defs)-1].used = true
					}
				}
			}

			globalPos += len(line)
		}

		for name, defs := range definitionsByName {
			for _, def := range defs {
				if !def.used {
					violations = append(violations, LinterRuleViolation{
						LineNumber:    def.lineNum,
						RuleID:        unusedVariablesID,
						ShortMessage:  fmt.Sprintf("variable %s is defined but not used within scope", name),
						Message:       fmt.Sprintf("Variable %s is defined but not used within scope.", name),
						FormattedLine: strings.TrimSpace(lines[def.lineNum-1]),
						Column:        bytePosToColumn(lines[def.lineNum-1], def.bytePos),
					})
				}
			}
		}
	}

	// Determine boundaries of object definition(s) by line number
	var blockStartLines []int
	if isRaw {
		blockStartLines = []int{0}
	} else {
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

	var hubTemplates []templateWithLine

	// Check for unused variables in each object definition scope
	for i, startLine := range blockStartLines {
		endLine := len(lines)
		if i+1 < len(blockStartLines) {
			endLine = blockStartLines[i+1]
		}

		blockLines := cleanedLines[startLine:endLine]
		lineOffset := startLine

		hubTmpls, managedTmpls := extractTmplsFromLines(blockLines, lineOffset)

		hubTemplates = append(hubTemplates, hubTmpls...)

		checkUnusedVars(managedTmpls)
	}

	// Check for unused variables in the hub templates scope
	checkUnusedVars(hubTemplates)

	return violations
}
