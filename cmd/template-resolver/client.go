// Copyright Contributors to the Open Cluster Management project

package main

import (
	"os"

	"github.com/stolostron/go-template-utils/v6/cmd/template-resolver/utils"
)

func main() {
	err := utils.Execute()
	if err != nil {
		os.Exit(1)
	}
}
