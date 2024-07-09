// Copyright Contributors to the Open Cluster Management project

package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/klog"

	templateresolver "github.com/stolostron/go-template-utils/v5/cmd/template-resolver/utils"
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
	}

	klog.InitFlags(nil)
	flag.StringVar(&hubKubeConfigPath, "hub-kubeconfig", "", "the input kubeconfig to also resolve hub templates")
	flag.StringVar(
		&clusterName, "cluster-name", "", "the cluster name to use as .ManagedClusterName when resolving hub templates",
	)
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintln(
			os.Stderr, "Exactly one positional argument of the YAML file to resolve templates must be provided",
		)
		os.Exit(1)
	}

	yamlFile := args[0]

	if hubKubeConfigPath != "" && clusterName == "" {
		fmt.Fprintln(
			os.Stderr,
			"When a hub kubeconfig is provided, you must provide a managed cluster name for hub templates to resolve "+
				"using the -cluster-name argument",
		)
		os.Exit(1)
	}

	templateresolver.ProcessTemplate(yamlFile, hubKubeConfigPath, clusterName)
}
