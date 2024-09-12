// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	sprig "github.com/Masterminds/sprig/v3"
)

var (
	sprigFuncMap = sprig.FuncMap()

	// exportedSprigFunctions lists all of the functions from sprig that will be exposed
	exportedSprigFunctions = []string{
		"add",
		"append",
		"cat",
		"concat",
		"contains",
		"date",
		"default",
		"dict",
		"dig",
		"div",
		"empty",
		"fromJson",
		"get",
		"has",
		"hasKey",
		"hasPrefix",
		"hasSuffix",
		"htpasswd",
		"join",
		"list",
		"lower",
		"merge",
		"mul",
		"mustAppend",
		"mustFromJson",
		"mustHas",
		"mustMerge",
		"mustPrepend",
		"mustRegexFind",
		"mustRegexFindAll",
		"mustRegexMatch",
		"mustSlice",
		"mustToDate",
		"mustToRawJson",
		"now",
		"prepend",
		"quote",
		"regexFind",
		"regexFindAll",
		"regexMatch",
		"regexQuoteMeta",
		"replace",
		"round",
		"semver",
		"semverCompare",
		"set",
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
		"unset",
		"until",
		"untilStep",
		"upper",
	}
)

func getSprigFunc(funcName string) (result interface{}) {
	return sprigFuncMap[funcName]
}

// AvailableSprigFunctions returns a copy of the list of functions that this
// library makes available from the Sprig library.
func AvailableSprigFunctions() []string {
	return append(make([]string, 0, len(exportedSprigFunctions)), exportedSprigFunctions...)
}
