// Copyright Contributors to the Open Cluster Management project

package main

import (
	"errors"
	"os"

	"github.com/stolostron/go-template-utils/v7/cmd/template-resolver/utils"
)

func main() {
	err := utils.Execute()
	if err != nil {
		var exitErr *utils.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}

		os.Exit(1)
	}
}
