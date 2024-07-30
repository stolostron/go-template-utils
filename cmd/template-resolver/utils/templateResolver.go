package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/stolostron/go-template-utils/v6/pkg/templates"
)

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

		if stdinInfo.Size() == 0 {
			return nil, fmt.Errorf("failed to read from stdin: input is empty")
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

// ProcessTemplate takes a YAML byte array input, unmarshals it to a Policy, processes the
// templates, and marshals it back to YAML, returning the resulting byte array. Validation is
// performed along the way, returning an error if any failures are found. It uses the
// `hubKubeConfigPath` and `clusterName` to establish a dynamic client with the hub to resolve any
// hub templates it finds.
func ProcessTemplate(yamlBytes []byte, hubKubeConfigPath, clusterName string) ([]byte, error) {
	policy := unstructured.Unstructured{}

	err := yaml.Unmarshal(yamlBytes, &policy.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input to YAML: %w", err)
	}

	if policy.GetKind() != "Policy" && policy.GetAPIVersion() != "policy.open-cluster-management.io/v1" {
		return nil, fmt.Errorf("the input YAML file is not a v1 Policy manifest")
	}

	policyTemplates, _, err := unstructured.NestedSlice(policy.Object, "spec", "policy-templates")
	if err != nil {
		return nil, fmt.Errorf("invalid policy-templates array was provided: %w", err)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

	kubeConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to determine the kubeconfig to use: %w", err)
	}

	var hubResolver *templates.TemplateResolver

	hubTemplateCtx := struct {
		ManagedClusterName   string
		ManagedClusterLabels map[string]string
	}{ManagedClusterName: clusterName}

	var hubResolveOptions templates.ResolveOptions

	if hubKubeConfigPath != "" {
		if policy.GetNamespace() == "" {
			return nil, fmt.Errorf("the input Policy manifest must specify a namespace for hub templates")
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

		hubTemplateCtx.ManagedClusterLabels = mc.GetLabels()

		hubTemplatesConfig := templates.Config{
			AdditionalIndentation: 8,
			DisabledFunctions:     []string{},
			StartDelim:            "{{hub",
			StopDelim:             "hub}}",
		}

		hubResolveOptions = templates.ResolveOptions{
			ClusterScopedAllowList: []templates.ClusterScopedObjectIdentifier{{
				Group: "cluster.open-cluster-management.io",
				Kind:  "ManagedCluster",
				Name:  clusterName,
			}},
			LookupNamespace: policy.GetNamespace(),
		}

		hubResolver, err = templates.NewResolver(hubKubeConfig, hubTemplatesConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate the hub template resolver: %w", err)
		}
	}

	resolver, err := templates.NewResolver(kubeConfig, templates.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate the template resolver: %w", err)
	}

	for i := range policyTemplates {
		policyTemplate, ok := policyTemplates[i].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid policy-templates entry was provided: %w", err)
		}

		objectDefinition, ok := policyTemplate["objectDefinition"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid policy-templates entry was provided: %w", err)
		}

		objectDefinitionUnstructured := unstructured.Unstructured{Object: objectDefinition}
		if objectDefinitionUnstructured.GetAPIVersion() != "policy.open-cluster-management.io/v1" {
			continue
		}

		if objectDefinitionUnstructured.GetKind() != "ConfigurationPolicy" {
			continue
		}

		if hubResolver != nil {
			objectDefinitionJSON, err := json.Marshal(objectDefinition)
			if err != nil {
				return nil, fmt.Errorf("invalid policy-templates entry at index %d: %w", i, err)
			}

			hubTemplateResult, err := hubResolver.ResolveTemplate(
				objectDefinitionJSON, hubTemplateCtx, &hubResolveOptions,
			)
			if err != nil {
				return nil, fmt.Errorf("invalid policy-templates entry at index %d: %w", i, err)
			}

			var resolvedObjectDefinition map[string]interface{}

			err = json.Unmarshal(hubTemplateResult.ResolvedJSON, &resolvedObjectDefinition)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid policy-templates entry at index %d after resolving templates: %w",
					i,
					err,
				)
			}

			err = unstructured.SetNestedField(policyTemplate, resolvedObjectDefinition, "objectDefinition")
			if err != nil {
				return nil, fmt.Errorf(
					"invalid policy-templates entry at index %d after resolving templates: %w",
					i,
					err,
				)
			}

			objectDefinition = policyTemplate["objectDefinition"].(map[string]interface{})
			objectDefinitionUnstructured.Object = objectDefinition
		}

		var rawDataList [][]byte

		resolveOptions := templates.ResolveOptions{}

		oTRaw, oTRawFound, _ := unstructured.NestedString(objectDefinition, "spec", "object-templates-raw")
		if oTRawFound {
			resolveOptions.InputIsYAML = true

			rawDataList = [][]byte{[]byte(oTRaw)}
		} else {
			resolveOptions.InputIsYAML = false

			objTemplates, _, err := unstructured.NestedSlice(objectDefinition, "spec", "object-templates")
			if err != nil {
				return nil, fmt.Errorf(
					"invalid object-templates array in ConfigurationPolicy at policy-templates index %d: %w",
					i,
					err,
				)
			}

			for _, objTemplate := range objTemplates {
				jsonBytes, err := json.Marshal(objTemplate)
				if err != nil {
					return nil, fmt.Errorf(
						"invalid object-templates array in ConfigurationPolicy at policy-templates index %d: %w",
						i,
						err,
					)
				}

				rawDataList = append(rawDataList, jsonBytes)
			}
		}

		objectTemplates := make([]interface{}, 0, len(rawDataList))

		for _, rawData := range rawDataList {
			if bytes.Contains(rawData, []byte("{{hub")) {
				return nil, fmt.Errorf(
					"unresolved hub template in ConfigurationPolicy at policy-templates index %d. "+
						"Use the -hub-kubeconfig argument",
					i,
				)
			}

			tmplResult, err := resolver.ResolveTemplate(rawData, nil, &resolveOptions)
			if err != nil {
				return nil, fmt.Errorf("failed to process the templates at policy-templates index %d: %w", i, err)
			}

			var resolvedOT interface{}

			err = json.Unmarshal(tmplResult.ResolvedJSON, &resolvedOT)
			if err != nil {
				return nil, fmt.Errorf("failed to process the templates at policy-templates index %d: %w", i, err)
			}

			if oTRawFound {
				switch v := resolvedOT.(type) {
				case []interface{}:
					objectTemplates = v
				case nil:
					objectTemplates = []interface{}{}
				default:
					return nil, fmt.Errorf(
						"object-templates-raw in policy-templates index %d was not an array after templates were "+
							"resolved",
						i,
					)
				}

				unstructured.RemoveNestedField(objectDefinition, "spec", "object-templates-raw")

				break
			}

			objectTemplates = append(objectTemplates, resolvedOT)
		}

		err = unstructured.SetNestedSlice(objectDefinition, objectTemplates, "spec", "object-templates")
		if err != nil {
			return nil, fmt.Errorf("failed to process the templates at policy-templates index %d: %w", i, err)
		}
	}

	err = unstructured.SetNestedSlice(policy.Object, policyTemplates, "spec", "policy-templates")
	if err != nil {
		return nil, fmt.Errorf("invalid policy-templates after resolving templates: %w", err)
	}

	resolvedPolicy, err := json.Marshal(policy.Object)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON resulted after resolving templates: %w", err)
	}

	resolvedYAML, err := templates.JSONToYAML(resolvedPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to convert the processed Policy back to YAML: %w", err)
	}

	return resolvedYAML, nil
}
