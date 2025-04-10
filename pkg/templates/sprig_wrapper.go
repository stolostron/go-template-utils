// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	sprig "github.com/Masterminds/sprig/v3"
)

var (
	sprigFuncMap = sprig.FuncMap()

	// exportedSprigFunctions lists all of the functions from sprig that will be exposed
	// ref: https://masterminds.github.io/sprig/
	exportedSprigFunctions = []string{
		// STRING
		"cat",
		"contains",
		"hasPrefix",
		"hasSuffix",
		"lower",
		"mustRegexFind",
		"mustRegexFindAll",
		"mustRegexMatch",
		"quote",
		"regexFind",
		"regexFindAll",
		"regexMatch",
		"regexQuoteMeta",
		"replace",
		"substr",
		"trim",
		"trimAll",
		"trunc",
		"upper",

		// STRING LIST
		"concat",
		"join",
		"split",
		"splitn",

		// INTEGER MATH
		"add",
		"div",
		"mul",
		"round",
		"sub",

		// INTEGER SLICE
		"until",
		"untilStep",

		// FLOAT MATH
		//   --

		// DATE
		"date",
		"mustToDate",
		"now",
		"toDate",

		// DEFAULTS
		"default",
		"empty",
		"fromJson",
		"mustFromJson",
		"mustToJson",
		"mustToRawJson",
		"ternary",
		"toJson",
		"toRawJson",

		// ENCODING
		//   "b64enc" -- Implemented locally
		//   "b64dec" -- Implemented locally

		// LISTS AND LIST
		"append",
		"has",
		"list",
		"mustAppend",
		"mustHas",
		"mustPrepend",
		"prepend",
		"mustSlice",
		"slice",

		// DICTIONARIES AND DICT
		"dict",
		"dig",
		"get",
		"hasKey",
		"merge",
		"mustMerge",
		"set",
		"unset",

		// TYPE CONVERSION
		//   "atoi" -- Implemented locally

		// PATH AND FILEPATH
		//   --

		// FLOW CONTROL
		"fail",

		// VERSION COMPARISON
		"semver",
		"semverCompare",

		// CRYPTOGRAPHIC AND SECURITY
		"htpasswd",
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
