// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"sort"

	sprig "github.com/Masterminds/sprig/v3"
)

var sprigFuncMap = sprig.FuncMap()

func getSprigFunc(funcName string) (result interface{}) {
	return sprigFuncMap[funcName]
}

// AvailableSprigFunctions returns a copy of the list of functions that this
// library makes available from the Sprig library. This returns all Sprig
// functions except those that are considered security-sensitive or
// non-deterministic (see sensitiveSprigFunctions).
func AvailableSprigFunctions() []string {
	funcNames := make([]string, 0, len(sprigFuncMap))

	for name := range sprigFuncMap {
		if isSensitiveSprigFunction(name) {
			continue
		}

		funcNames = append(funcNames, name)
	}

	// Sort the list so the output is stable for consumers.
	sort.Strings(funcNames)

	return funcNames
}
