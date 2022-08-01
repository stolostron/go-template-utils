// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	sprig "github.com/Masterminds/sprig/v3"
)

var (
	sprigFuncMap = sprig.FuncMap()

	// ExportedSprigFunctions lists all of the functions from sprig that will be exposed
	exportedSprigFunctions = []string{
		"cat",
		"contains",
		"default",
		"empty",
		"fromJson",
		"hasPrefix",
		"hasSuffix",
		"join",
		"list",
		"lower",
		"mustFromJson",
		"quote",
		"replace",
		"semver",
		"semverCompare",
		"split",
		"splitn",
		"ternary",
		"trim",
		"until",
		"untilStep",
		"upper",
	}
)

func getSprigFunc(funcName string) (result interface{}) {
	return sprigFuncMap[funcName]
}
