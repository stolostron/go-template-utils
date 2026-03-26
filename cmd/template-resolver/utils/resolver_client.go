package utils

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/stolostron/go-template-utils/v7/pkg/lint"
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
	SaveHubResources     string `yaml:"saveHubResources"`
	LocalResources       string `yaml:"localResources"`
	SkipPolicyValidation bool   `yaml:"skipPolicyValidation"`
	Lint                 bool   `yaml:"lint"`
	SarifOutput          string `yaml:"sarif"`
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

	templateResolverCmd.AddCommand(t.getLintCmd())

	// Initialize variables used to collect user input from CLI flags
	// Add template-resolver command and parse flags
	templateResolverCmd.Flags().StringVar(
		&t.HubKubeConfigPath,
		"hub-kubeconfig",
		"",
		"The input kubeconfig to also resolve hub templates.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.LocalResources,
		"local-resources",
		"",
		"The path to a local YAML manifest used to resolve the managed cluster template. "+
			"The YAML manifest can define multiple resources.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.ClusterName,
		"cluster-name",
		"",
		"The cluster name to use for the .ManagedClusterName template variable when resolving hub cluster templates.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.HubNamespace,
		"hub-namespace",
		"",
		"The namespace on the hub to restrict namespaced lookups to when resolving hub templates.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.ObjNamespace,
		"object-namespace",
		"",
		"The object namespace to use for the .ObjectNamespace template variable when policy uses namespaceSelector.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.ObjName,
		"object-name",
		"",
		"The object namespace to use for the .ObjectName template variable "+
			"when policy uses namespaceSelector or objectSelector.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.SaveResources,
		"save-resources",
		"",
		"The path to save the output containing resources used. "+
			"This output can be used as input resources for the dry-run CLI or for local environment testing.",
	)

	templateResolverCmd.Flags().StringVar(
		&t.SaveHubResources,
		"save-hub-resources",
		"",
		"The path to save the output containing resources used in the hub cluster. "+
			"This output can be used as input resources for the dry-run CLI or for local environment testing.",
	)

	templateResolverCmd.Flags().BoolVar(
		&t.SkipPolicyValidation,
		"skip-policy-validation",
		false,
		"Handle the input directly as a Go template, skipping any surrounding policy field validations.",
	)

	envVarLint := "TEMPLATE_RESOLVER_LINT"
	lint := os.Getenv(envVarLint) == "true"

	templateResolverCmd.Flags().BoolVar(
		&t.Lint,
		"lint",
		lint,
		fmt.Sprintf(
			"(Tech Preview) Lint the template string for issues (Alternatively, set the environment variable %s=true).",
			envVarLint),
	)

	templateResolverCmd.Flags().StringVar(
		&t.SarifOutput,
		"sarif",
		"",
		"(Tech Preview) Location to save a SARIF report of the lint results. Requires --lint to be true.",
	)

	return templateResolverCmd
}

func (t *TemplateResolver) getLintCmd() *cobra.Command {
	lintCmd := &cobra.Command{
		Use: `lint [flags] [file|-]

  The file positional argument is the path to a policy YAML manifest. If file
  is a dash ('-') or absent, template-resolver reads from the standard input.`,
		Short: "Lint Policy templates",
		Long:  "Lint Policy templates",
		Args:  cobra.MaximumNArgs(1),
		RunE:  t.runLint,
	}

	lintCmd.Flags().StringVar(
		&t.SarifOutput,
		"sarif",
		"",
		"(Tech Preview) Location to save a SARIF report of the lint results.",
	)

	return lintCmd
}

func runLint(cmd *cobra.Command, yamlFile string, yamlBytes []byte, sarifOutput string) error {
	violations := lint.Lint(string(yamlBytes))
	if len(violations) > 0 {
		cmd.Println("Found linting issues:")
		cmd.Print(lint.OutputStringViolations(violations))
	}

	if sarifOutput == "" {
		return nil
	}

	inputFile := yamlFile
	if inputFile == "" || inputFile == "-" {
		inputFile = "<stdin>"
	}

	file, err := os.Create(sarifOutput) //#nosec G304 -- files accessed here are on the user's local system
	if err != nil {
		return fmt.Errorf("failed to open output SARIF report file: %w", err)
	}

	defer file.Close()

	if err := lint.OutputSARIF(violations, inputFile, file); err != nil {
		return fmt.Errorf("failed to write SARIF report: %w", err)
	}

	return nil
}

func (t *TemplateResolver) runLint(cmd *cobra.Command, args []string) error {
	yamlFile, yamlBytes, err := getInputYAML(args)
	if err != nil {
		return err
	}

	return runLint(cmd, yamlFile, yamlBytes, t.SarifOutput)
}

func (t *TemplateResolver) resolveTemplates(cmd *cobra.Command, args []string) error {
	// Validate flag args
	if t.HubKubeConfigPath != "" && t.ClusterName == "" {
		return errors.New(
			"when a hub kubeconfig is provided, you must provide a managed cluster name for hub templates to resolve " +
				"using the cluster-name argument",
		)
	}

	yamlFile, yamlBytes, err := getInputYAML(args)
	if err != nil {
		return err
	}

	if t.Lint {
		if err := runLint(cmd, yamlFile, yamlBytes, t.SarifOutput); err != nil {
			return err
		}

		cmd.Println()
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
