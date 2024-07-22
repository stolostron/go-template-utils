// Copyright Contributors to the Open Cluster Management project

package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/klog"

	templateresolver "github.com/stolostron/go-template-utils/v6/cmd/template-resolver/utils"
)

func main() {
	var hubKubeConfigPath, clusterName string

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, `Usage: template-resolver [OPTIONS] [path to YAML]
  -cluster-name string
    	the cluster name to use as .ManagedClusterName when resolving hub templates
  -hub-kubeconfig string
    	the input kubeconfig to also resolve hub templates
  -v value
    	number for the log level verbosity`)

		os.Exit(0)
	}

	// Handle flag arguments
	klog.InitFlags(nil)
	flag.StringVar(&hubKubeConfigPath, "hub-kubeconfig", "", "the input kubeconfig to also resolve hub templates")
	flag.StringVar(
		&clusterName, "cluster-name", "", "the cluster name to use as .ManagedClusterName when resolving hub templates",
	)
	flag.Parse()

	// Handle file path positional argument
	yamlFile := ""
	args := flag.Args()

	// Validate the file path positional argument--an empty argument or `-` reads input from `stdin`
	if len(args) > 1 {
		errAndExit(
			"Exactly one positional argument of the YAML file to resolve templates must be provided. " +
				"Use a hyphen (\"-\") to read from stdin.",
		)
	}

	// Print usage if no arguments are given and stdin isn't detected
	if len(args) == 0 {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			errAndExit(fmt.Sprintf("Error reading stdin: %v", err))
		}

		if (stdinInfo.Mode() & os.ModeCharDevice) != 0 {
			flag.Usage()
		}
	}

	// Set YAML path if a positional argument is provided
	if len(args) == 1 {
		yamlFile = args[0]
	}

	if hubKubeConfigPath != "" && clusterName == "" {
		errAndExit(
			"When a hub kubeconfig is provided, you must provide a managed cluster name for hub templates to resolve " +
				"using the -cluster-name argument",
		)
	}

	yamlBytes, err := templateresolver.HandleFile(yamlFile)
	if err != nil {
		errAndExit(fmt.Sprintf("Error handling YAML file input: %v", err))
	}

	resolvedYAML, err := templateresolver.ProcessTemplate(yamlBytes, hubKubeConfigPath, clusterName)
	if err != nil {
		errAndExit(fmt.Sprintf("Error processing templates: %v", err))
	}

	//nolint: forbidigo
	fmt.Print(string(resolvedYAML))
}

// errAndExit prints an error message and exits with exit code 1
func errAndExit(err string) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
