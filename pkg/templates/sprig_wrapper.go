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
		"add",
		"append",
		"cat",
		"concat",
		"contains",
		"date",
		"default",
		"dig",
		"div",
		"empty",
		"fromJson",
		"has",
		"hasPrefix",
		"hasSuffix",
		"htpasswd",
		"join",
		"list",
		"lower",
		"mul",
		"mustAppend",
		"mustFromJson",
		"mustHas",
		"mustPrepend",
		"mustSlice",
		"mustToDate",
		"mustToRawJson",
		"now",
		"prepend",
		"quote",
		"replace",
		"round",
		"semver",
		"semverCompare",
		"slice",
		"split",
		"splitn",
		"sub",
		"substr",
		"ternary",
		"toDate",
		"toRawJson",
		"trim",
		"trimAll",
		"trunc",
		"until",
		"untilStep",
		"upper",
	}
)

func getSprigFunc(funcName string) (result interface{}) {
	return sprigFuncMap[funcName]
}
