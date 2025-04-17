package utils

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Struct representing the template-resolver command
type TemplateResolver struct {
	HubKubeConfigPath string `yaml:"hubKubeConfigPath"`
	ClusterName       string `yaml:"clusterName"`
	HubNamespace      string `yaml:"hubNamespace"`
	ObjNamespace      string `yaml:"objNamespace"`
	ObjName           string `yaml:"objName"`
	SaveResources     string `yaml:"saveResources"`
	// saveHubResources Output doesn't include "ManagedClusters" resources
	SaveHubResources string `yaml:"saveHubResources"`
}

func (t *TemplateResolver) GetCmd() *cobra.Command {
	// templateResolverCmd represents the template-resolver command
	templateResolverCmd := &cobra.Command{
		Use: `template-resolver [flags] [file|-]

  The file positional argument is the path to a policy YAML manifest. If file 
  is a dash ('-') or absent, template-resolver reads from the standard input.`,
		Short: "Locally resolve Policy templates",
		Long:  "Locally resolve Policy templates",
		Args:  cobra.MaximumNArgs(1),
		RunE:  t.resolveTemplates,
	}

	// Initialize variables used to collect user input from CLI flags
	// Add template-resolver command and parse flags
	templateResolverCmd.Flags().StringVar(
		&t.HubKubeConfigPath,
		"hub-kubeconfig",
		"",
		"the input kubeconfig to also resolve hub templates",
	)

	templateResolverCmd.Flags().StringVar(
		&t.ClusterName,
		"cluster-name",
		"",
		"the cluster name to use for the .ManagedClusterName template variable when resolving hub cluster templates",
	)

	templateResolverCmd.Flags().StringVar(
		&t.HubNamespace,
		"hub-namespace",
		"",
		"the namespace on the hub to restrict namespaced lookups to when resolving hub templates",
	)

	templateResolverCmd.Flags().StringVar(
		&t.ObjNamespace,
		"object-namespace",
		"",
		"the object namespace to use for the .ObjectNamespace template variable when policy uses namespaceSelector",
	)

	templateResolverCmd.Flags().StringVar(
		&t.ObjName,
		"object-name",
		"",
		"the object namespace to use for the .ObjectName template variable "+
			"when policy uses namespaceSelector or objectSelector",
	)

	templateResolverCmd.Flags().StringVar(
		&t.SaveResources,
		"save-resources",
		"",
		"the path to save the output containing resources used. "+
			"This output can be used as input resources for the dry-run CLI or for local environment testing.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.SaveHubResources,
		"save-hub-resources",
		"",
		"the path to save the output containing resources used in the hub cluster. "+
			"This output can be used as input resources for the dry-run CLI or for local environment testing.",
	)

	return templateResolverCmd
}

func (t *TemplateResolver) resolveTemplates(cmd *cobra.Command, args []string) error {
	// Validate YAML input as positional arg
	yamlFile := ""

	// Detect whether stdin is provided when no arguments are provided
	if len(args) == 0 {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			return fmt.Errorf("error reading stdin: %w", err)
		}

		if (stdinInfo.Mode() & os.ModeCharDevice) != 0 {
			return errors.New("failed to read from stdin: input is not a pipe")
		}
	}

	// Set YAML path if a positional argument is provided ("-" is read as stdin)
	if len(args) == 1 {
		yamlFile = args[0]
	}

	// Validate flag args
	if t.HubKubeConfigPath != "" && t.ClusterName == "" {
		return errors.New(
			"when a hub kubeconfig is provided, you must provide a managed cluster name for hub templates to resolve " +
				"using the cluster-name argument",
		)
	}

	yamlBytes, err := HandleFile(yamlFile)
	if err != nil {
		return fmt.Errorf("error handling YAML file input: %w", err)
	}

	resolvedYAML, err := t.ProcessTemplate(yamlBytes)
	if err != nil {
		cmd.Printf("error processing templates: %s\n", err.Error())

		os.Exit(2)
	}

	cmd.SetOut(os.Stdout)
	cmd.Print(string(resolvedYAML))

	return nil
}

// Execute runs the `template-resolver` command.
func Execute() error {
	tmplResolverCmd := TemplateResolver{}

	return tmplResolverCmd.GetCmd().Execute()
}
