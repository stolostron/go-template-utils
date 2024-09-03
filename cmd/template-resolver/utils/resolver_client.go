package utils

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Struct representing the templateresolver command
type TemplateResolver struct {
	hubKubeConfigPath string
	clusterName       string
}

func (t *TemplateResolver) GetCmd() *cobra.Command {
	// templateResolverCmd represents the templateresolver command
	templateResolverCmd := &cobra.Command{
		Use:   "templateresolver",
		Short: "Locally resolve Policy templates",
		Long:  "Locally resolve Policy templates",
		Args:  cobra.MaximumNArgs(1),
		RunE:  t.resolveTemplates,
	}

	// Initialize variables used to collect user input from CLI flags
	// Add templateresolver command and parse flags
	templateResolverCmd.Flags().StringVar(
		&t.hubKubeConfigPath,
		"hub-kubeconfig",
		"",
		"the input kubeconfig to also resolve hub templates",
	)
	templateResolverCmd.Flags().StringVar(
		&t.clusterName,
		"cluster-name",
		"",
		"the cluster name to use as .ManagedClusterName when resolving hub templates",
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
			err := cmd.Usage()

			return err
		}
	}

	// Set YAML path if a positional argument is provided ("-" is read as stdin)
	if len(args) == 1 {
		yamlFile = args[0]
	}

	// Validate flag args
	if t.hubKubeConfigPath != "" && t.clusterName == "" {
		return fmt.Errorf(
			"when a hub kubeconfig is provided, you must provide a managed cluster name for hub templates to resolve " +
				"using the -cluster-name argument",
		)
	}

	yamlBytes, err := HandleFile(yamlFile)
	if err != nil {
		return fmt.Errorf("error handling YAML file input: %w", err)
	}

	resolvedYAML, err := ProcessTemplate(yamlBytes, t.hubKubeConfigPath, t.clusterName)
	if err != nil {
		return fmt.Errorf("error processing templates: %w", err)
	}

	//nolint:forbidigo
	fmt.Print(string(resolvedYAML))

	return nil
}

// Execute runs the `templateresolver` command.
func Execute() error {
	tmplResolverCmd := TemplateResolver{}

	return tmplResolverCmd.GetCmd().Execute()
}
