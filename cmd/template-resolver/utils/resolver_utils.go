package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/stolostron/go-template-utils/v6/pkg/templates"
)

type hubTemplateCtx struct {
	ManagedClusterName   string
	ManagedClusterLabels map[string]string
	PolicyMetadata       map[string]interface{}
}

type hubTemplateOptions struct {
	config templates.Config
	opts   templates.ResolveOptions
	ctx    hubTemplateCtx
}

// HandleFile takes a file path and returns the resulting byte array. If an
// empty string ("") or hyphen ("-") is provided, input is read from stdin.
func HandleFile(yamlFile string) ([]byte, error) {
	var inputReader io.Reader

	// Handle stdin input given a hyphen, otherwise assume it's a file path
	if yamlFile == "" || yamlFile == "-" {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}

		if stdinInfo.Size() == 0 && (stdinInfo.Mode()&os.ModeNamedPipe) == 0 {
			return nil, errors.New("failed to read from stdin: input is empty")
		}

		inputReader = os.Stdin
		yamlFile = "<stdin>"
	} else {
		var err error

		// #nosec G304 -- Reading in a file is required for the tool to work.
		inputReader, err = os.Open(yamlFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read the file \"%s\": %w", yamlFile, err)
		}
	}

	yamlBytes, err := io.ReadAll(inputReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read the file \"%s\": %w", yamlFile, err)
	}

	return yamlBytes, nil
}

// ProcessTemplate takes a YAML byte array input, unmarshals it to a Policy, ConfigPolicy,
// or object-templates-raw, processes the templates, and marshals it back to YAML,
// returning the resulting byte array. Validation is performed along the way, returning
// an error if any failures are found. It uses the `hubKubeConfigPath`, `hubNS` and `clusterName`
// to establish a dynamic client with the hub to resolve any hub templates it finds.
func ProcessTemplate(yamlBytes []byte, hubKubeConfigPath, clusterName, hubNS string) ([]byte, error) {
	policy := unstructured.Unstructured{}

	err := k8syaml.Unmarshal(yamlBytes, &policy.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input to YAML: %w", err)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

	kubeConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to determine the kubeconfig to use: %w", err)
	}

	hubTemplateOpts := &hubTemplateOptions{
		config: templates.Config{
			AdditionalIndentation: 8,
			DisabledFunctions:     []string{},
			StartDelim:            "{{hub",
			StopDelim:             "hub}}",
		},
	}

	var hubResolver *templates.TemplateResolver

	if hubKubeConfigPath != "" {
		customSA, _, _ := unstructured.NestedString(policy.Object, "spec", "hubTemplateOptions", "serviceAccountName")

		if customSA == "" {
			if policy.GetKind() == "Policy" {
				// neither specified
				if hubNS == "" && policy.GetNamespace() == "" {
					return nil, errors.New("a namespace must be specified for hub templates, " +
						"either in the input Policy or as an argument if spec.hubTemplateOptions.serviceAccountName " +
						"is not specified")
				}

				// both specified and don't match
				if hubNS != "" && policy.GetNamespace() != "" && hubNS != policy.GetNamespace() {
					return nil, errors.New("the namespace specified in the Policy and the " +
						"hub-namespace argument must match")
				}

				// either hubNS is already specified, or we'll use the one in the policy
				if policy.GetNamespace() != "" {
					hubNS = policy.GetNamespace()
				}
			} else if hubNS == "" {
				// Non-Policy types just always require the argument
				return nil, errors.New("a hub namespace must be provided when a hub kubeconfig is provided " +
					"and spec.hubTemplateOptions.serviceAccountName is not specified")
			}
		}

		hubKubeConfig, err := clientcmd.BuildConfigFromFlags("", hubKubeConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load the Hub kubeconfig: %w", err)
		}

		dynamicHubClient, err := dynamic.NewForConfig(hubKubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the hub cluster: %w", err)
		}

		mcGVR := schema.GroupVersionResource{
			Group:    "cluster.open-cluster-management.io",
			Version:  "v1",
			Resource: "managedclusters",
		}

		mc, err := dynamicHubClient.Resource(mcGVR).Get(context.TODO(), clusterName, v1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get the ManagedCluster object for %s: %w", clusterName, err)
		}

		if policy.GetKind() == "Policy" {
			hubTemplateOpts.ctx.PolicyMetadata = map[string]interface{}{
				"annotations": policy.GetAnnotations(),
				"labels":      policy.GetLabels(),
				"name":        policy.GetName(),
				"namespace":   policy.GetNamespace(),
			}
		}

		hubTemplateOpts.ctx.ManagedClusterName = clusterName
		hubTemplateOpts.ctx.ManagedClusterLabels = mc.GetLabels()

		// If a custom service account is provided, assume the hub kubeconfig is for that service account
		if customSA == "" {
			hubTemplateOpts.opts = templates.ResolveOptions{
				ClusterScopedAllowList: []templates.ClusterScopedObjectIdentifier{{
					Group: "cluster.open-cluster-management.io",
					Kind:  "ManagedCluster",
					Name:  clusterName,
				}},
				LookupNamespace: hubNS,
			}
		}

		hubResolver, err = templates.NewResolver(hubKubeConfig, hubTemplateOpts.config)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate the hub template resolver: %w", err)
		}

		hubResolvedObject, err := resolveHubTemplates(policy.Object, hubResolver, hubTemplateOpts)
		if err != nil {
			return nil, err
		}

		policy.Object = hubResolvedObject
	}

	resolver, err := templates.NewResolver(kubeConfig, templates.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate the template resolver: %w", err)
	}

	switch policy.GetKind() {
	case "Policy":
		err = processPolicyTemplate(&policy, resolver)
	case "ConfigurationPolicy":
		err = processConfigPolicyTemplate(&policy, resolver)
	case "OperatorPolicy":
		_, err = processOperatorPolicyTemplates(policy.Object, resolver)
	default:
		if _, ok := policy.Object["object-templates-raw"]; !ok {
			return nil, errors.New("invalid YAML. Supported types: Policy, " +
				"ConfigurationPolicy, object-templates-raw")
		}

		err = processObjTemplatesRaw(&policy, resolver)
	}

	if err != nil {
		return nil, err
	}

	resolvedJSON, err := json.Marshal(policy.Object)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON resulted after resolving templates: %w", err)
	}

	resolvedYAML, err := templates.JSONToYAML(resolvedJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to convert the processed object back to YAML: %w", err)
	}

	return resolvedYAML, nil
}

// ProcessPolicyTemplate takes the unmarshalled Policy YAML as input and resolves
// all valid ConfigurationPolicy templates specified in the policy-templates field
func processPolicyTemplate(
	policy *unstructured.Unstructured,
	resolver *templates.TemplateResolver,
) error {
	policyTemplates, found, err := unstructured.NestedSlice(policy.Object, "spec", "policy-templates")
	if err != nil {
		return fmt.Errorf("invalid policy-templates array was provided: %w", err)
	} else if !found {
		return errors.New("invalid policy-templates array was provided: spec.policy-templates keys not found")
	}

	for i := range policyTemplates {
		policyTemplate, ok := policyTemplates[i].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid policy-templates entry was provided at index %d: "+
				"could not parse to map[string]interface{}", i)
		}

		objectDefinition, found, err := unstructured.NestedMap(policyTemplate, "objectDefinition")
		if err != nil {
			return fmt.Errorf("invalid policy-templates entry was provided at index %d: %w", i, err)
		} else if !found {
			return fmt.Errorf("invalid policy-templates entry was provided at index %d: "+
				"objectDefinition key not found", i)
		}

		templateObj := unstructured.Unstructured{Object: objectDefinition}

		switch templateObj.GetAPIVersion() {
		case "policy.open-cluster-management.io/v1":
			if templateObj.GetKind() != "ConfigurationPolicy" {
				continue
			}

			objectDefinition, err = processObjectTemplates(objectDefinition, resolver)
			if err != nil {
				return fmt.Errorf("%w (in policy-templates at index %d)", err, i)
			}
		case "policy.open-cluster-management.io/v1beta1":
			if templateObj.GetKind() != "OperatorPolicy" {
				continue
			}

			objectDefinition, err = processOperatorPolicyTemplates(objectDefinition, resolver)
			if err != nil {
				return fmt.Errorf("%w (in policy-templates at index %d)", err, i)
			}
		default:
			continue
		}

		err = unstructured.SetNestedField(policyTemplate, objectDefinition, "objectDefinition")
		if err != nil {
			return fmt.Errorf(
				"invalid policy-templates entry at index %d after resolving templates: %w",
				i,
				err,
			)
		}
	}

	err = unstructured.SetNestedSlice(policy.Object, policyTemplates, "spec", "policy-templates")
	if err != nil {
		return fmt.Errorf("invalid policy-templates after resolving templates: %w", err)
	}

	return nil
}

// ProcessConfigPolicyTemplate takes the unmarshalled ConfigPolicy YAML as input
// and resolves its templates
func processConfigPolicyTemplate(
	policy *unstructured.Unstructured,
	resolver *templates.TemplateResolver,
) error {
	resolvedPolicy, err := processObjectTemplates(policy.Object, resolver)
	if err != nil {
		return err
	}

	policy.Object = resolvedPolicy

	return nil
}

// processObjTemplatesRaw takes a YAML string representation and resolves the object's managed templates
func processObjTemplatesRaw(
	raw *unstructured.Unstructured,
	resolver *templates.TemplateResolver,
) error {
	resolveOptions := templates.ResolveOptions{InputIsYAML: true}

	oTRaw, _, _ := unstructured.NestedString(raw.Object, "object-templates-raw")
	if oTRaw == "" {
		return errors.New("invalid object-templates-raw after resolving hub templates")
	}

	if strings.Contains(oTRaw, "{{hub") {
		return errors.New("unresolved hub template in YAML input. Use the hub-kubeconfig argument")
	}

	tmplResult, err := resolver.ResolveTemplate([]byte(oTRaw), nil, &resolveOptions)
	if err != nil {
		return fmt.Errorf("failed to process the templates: %w", err)
	}

	var resolved interface{}

	err = json.Unmarshal(tmplResult.ResolvedJSON, &resolved)
	if err != nil {
		return fmt.Errorf("failed to process the templates: %w", err)
	}

	var objectTemplates []interface{}

	switch v := resolved.(type) {
	case []interface{}:
		objectTemplates = v
	case nil:
		objectTemplates = []interface{}{}
	default:
		return errors.New("object-templates-raw was not an array after templates were resolved")
	}

	unstructured.RemoveNestedField(raw.Object, "object-templates-raw")

	err = unstructured.SetNestedSlice(raw.Object, objectTemplates, "object-templates")
	if err != nil {
		return fmt.Errorf("failed to process the object-templates-raw: %w", err)
	}

	return nil
}

// processObjectTemplates takes any nested object and resolves its managed templates
func processObjectTemplates(
	objectDefinition map[string]interface{},
	resolver *templates.TemplateResolver,
) (map[string]interface{}, error) {
	_, oTRawFound, _ := unstructured.NestedString(objectDefinition, "spec", "object-templates-raw")
	if oTRawFound {
		policy := unstructured.Unstructured{Object: objectDefinition["spec"].(map[string]interface{})}

		err := processObjTemplatesRaw(&policy, resolver)
		if err != nil {
			return nil, err
		}

		objectDefinition["spec"] = policy.Object

		return objectDefinition, nil
	}

	objTemplates, _, err := unstructured.NestedSlice(objectDefinition, "spec", "object-templates")
	if err != nil {
		return nil, fmt.Errorf("invalid object-templates array in Configuration Policy: %w", err)
	}

	resolvedTemplates := make([]interface{}, len(objTemplates))
	resolveOptions := templates.ResolveOptions{InputIsYAML: false}

	for i, objTemplate := range objTemplates {
		fieldName := fmt.Sprintf("object-templates[%v]", i)

		resolved, err := resolveManagedTemplate(objTemplate, fieldName, resolver, resolveOptions)
		if err != nil {
			return nil, err
		}

		resolvedTemplates[i] = resolved
	}

	err = unstructured.SetNestedSlice(objectDefinition, resolvedTemplates, "spec", "object-templates")
	if err != nil {
		return nil, fmt.Errorf("failed to process the templates: %w", err)
	}

	return objectDefinition, nil
}

func processOperatorPolicyTemplates(
	operatorPolicy map[string]interface{},
	resolver *templates.TemplateResolver,
) (map[string]interface{}, error) {
	resolveOptions := templates.ResolveOptions{
		InputIsYAML: false,
	}

	opGroup, found, err := unstructured.NestedMap(operatorPolicy, "spec", "operatorGroup")
	if err != nil {
		return nil, fmt.Errorf("invalid operatorGroup: %w", err)
	}

	if found {
		resolved, err := resolveManagedTemplate(opGroup, "operatorGroup", resolver, resolveOptions)
		if err != nil {
			return nil, err
		}

		err = unstructured.SetNestedField(operatorPolicy, resolved, "spec", "operatorGroup")
		if err != nil {
			return nil, err
		}
	}

	sub, found, err := unstructured.NestedMap(operatorPolicy, "spec", "subscription")
	if err != nil {
		return nil, fmt.Errorf("invalid subscription: %w", err)
	}

	if found {
		resolved, err := resolveManagedTemplate(sub, "subscription", resolver, resolveOptions)
		if err != nil {
			return nil, err
		}

		err = unstructured.SetNestedField(operatorPolicy, resolved, "spec", "subscription")
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("spec.subscription must be set in OperatorPolicies")
	}

	return operatorPolicy, nil
}

// resolveHubTemplates takes a hub templateResolver and any nested object and resolves its hub templates
func resolveHubTemplates(
	objectDefinition map[string]interface{},
	hubResolver *templates.TemplateResolver,
	hubTemplateOpts *hubTemplateOptions,
) (map[string]interface{}, error) {
	objectDefinitionJSON, err := json.Marshal(objectDefinition)
	if err != nil {
		return nil, fmt.Errorf("invalid object: %w", err)
	}

	hubTemplateResult, err := hubResolver.ResolveTemplate(
		objectDefinitionJSON, hubTemplateOpts.ctx, &hubTemplateOpts.opts,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid object: %w", err)
	}

	var resolvedObjectDefinition map[string]interface{}

	err = json.Unmarshal(hubTemplateResult.ResolvedJSON, &resolvedObjectDefinition)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid object after resolving hub templates: %w", err,
		)
	}

	return resolvedObjectDefinition, nil
}

// resolveManagedTemplate resolves a template, and emits an error if any
// hub templates are still in the object.
func resolveManagedTemplate(
	field interface{},
	fieldName string,
	resolver *templates.TemplateResolver,
	resolveOptions templates.ResolveOptions,
) (interface{}, error) {
	rawData, err := json.Marshal(field)
	if err != nil {
		return nil, fmt.Errorf("invalid %v: %w", fieldName, err)
	}

	if bytes.Contains(rawData, []byte("{{hub")) {
		return nil, errors.New("unresolved hub template in YAML input. Use the hub-kubeconfig argument")
	}

	tmplResult, err := resolver.ResolveTemplate(rawData, nil, &resolveOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to process the templates: %w", err)
	}

	var resolved interface{}

	err = json.Unmarshal(tmplResult.ResolvedJSON, &resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to process the templates: %w", err)
	}

	return resolved, nil
}
