// Copyright Contributors to the Open Cluster Management project

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"sigs.k8s.io/yaml"

	"github.com/stolostron/go-template-utils/v4/pkg/templates"
)

func main() {
	klog.InitFlags(nil)

	var hubKubeConfigPath, clusterName string

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

	processTemplate(yamlFile, hubKubeConfigPath, clusterName)
}

func processTemplate(yamlFile, hubKubeConfigPath, clusterName string) {
	if yamlFile == "" {
		fmt.Fprintln(os.Stderr, "Please specify an input YAML file using -i")
		os.Exit(1)
	}

	// #nosec G304 -- Reading in a file is required for the tool to work.
	yamlBytes, err := os.ReadFile(yamlFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read the file \"%s\": %v\n", yamlFile, err)
		os.Exit(1)
	}

	policy := unstructured.Unstructured{}

	err = yaml.Unmarshal(yamlBytes, &policy.Object)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse the YAML in the file \"%s\": %v\n", yamlFile, err)
		os.Exit(1)
	}

	if policy.GetKind() != "Policy" && policy.GetAPIVersion() != "policy.open-cluster-management.io/v1" {
		fmt.Fprintf(os.Stderr, "The input YAML file is not a v1 Policy manifest\n")
		os.Exit(1)
	}

	policyTemplates, _, err := unstructured.NestedSlice(policy.Object, "spec", "policy-templates")
	if err != nil {
		fmt.Fprintf(os.Stderr, "An invalid policy-templates array was provided: %v\n", err)
		os.Exit(1)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

	kubeConfig, err := clientConfig.ClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to determine the kubeconfig to use: %v\n", err)
		os.Exit(1)
	}

	var hubResolver *templates.TemplateResolver

	hubTemplateCtx := struct {
		ManagedClusterName   string
		ManagedClusterLabels map[string]string
	}{ManagedClusterName: clusterName}

	var hubResolveOptions templates.ResolveOptions

	if hubKubeConfigPath != "" {
		if policy.GetNamespace() == "" {
			fmt.Fprintf(os.Stderr, "The input Policy manifest must specify a namespace for hub templates\n")
			os.Exit(1)
		}

		hubKubeConfig, err := clientcmd.BuildConfigFromFlags("", hubKubeConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load the Hub kubeconfig: %v\n", err)
			os.Exit(1)
		}

		dynamicHubClient, err := dynamic.NewForConfig(hubKubeConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to connect to the hub cluster: %v\n", err)
			os.Exit(1)
		}

		mcGVR := schema.GroupVersionResource{
			Group:    "cluster.open-cluster-management.io",
			Version:  "v1",
			Resource: "managedclusters",
		}

		mc, err := dynamicHubClient.Resource(mcGVR).Get(context.TODO(), clusterName, v1.GetOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get the ManagedCluster object for %s: %v\n", clusterName, err)
			os.Exit(1)
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
			fmt.Fprintf(os.Stderr, "Failed to instantiate the hub template resolver: %v\n", err)
			os.Exit(1)
		}
	}

	resolver, err := templates.NewResolver(kubeConfig, templates.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to instantiate the template resolver: %v\n", err)
		os.Exit(1)
	}

	for i := range policyTemplates {
		policyTemplate, ok := policyTemplates[i].(map[string]interface{})
		if !ok {
			fmt.Fprintf(os.Stderr, "An invalid policy-templates entry was provided: %v\n", err)
			os.Exit(1)
		}

		objectDefinition, ok := policyTemplate["objectDefinition"].(map[string]interface{})
		if !ok {
			fmt.Fprintf(os.Stderr, "An invalid policy-templates entry was provided: %v\n", err)
			os.Exit(1)
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
				fmt.Fprintf(os.Stderr, "An invalid policy-templates entry at index %d was provided: %v\n", i, err)
				os.Exit(1)
			}

			hubTemplateResult, err := hubResolver.ResolveTemplate(
				objectDefinitionJSON, hubTemplateCtx, &hubResolveOptions,
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "An invalid policy-templates entry at index %d was provided: %v\n", i, err)
				os.Exit(1)
			}

			var resolvedObjectDefinition map[string]interface{}

			err = json.Unmarshal(hubTemplateResult.ResolvedJSON, &resolvedObjectDefinition)
			if err != nil {
				fmt.Fprintf(
					os.Stderr,
					"An invalid policy-templates entry at index %d after resolving templates: %v\n",
					i,
					err,
				)
				os.Exit(1)
			}

			err = unstructured.SetNestedField(policyTemplate, resolvedObjectDefinition, "objectDefinition")
			if err != nil {
				fmt.Fprintf(
					os.Stderr,
					"An invalid policy-templates entry at index %d after resolving templates: %v\n",
					i,
					err,
				)
				os.Exit(1)
			}

			objectDefinition = policyTemplate["objectDefinition"].(map[string]interface{})
			objectDefinitionUnstructured.Object = objectDefinition
		}

		var rawDataList [][]byte

		oTRaw, oTRawFound, _ := unstructured.NestedString(objectDefinition, "spec", "object-templates-raw")
		if oTRawFound {
			resolver.SetInputIsYAML(true)

			rawDataList = [][]byte{[]byte(oTRaw)}
		} else {
			resolver.SetInputIsYAML(false)

			objTemplates, _, err := unstructured.NestedSlice(objectDefinition, "spec", "object-templates")
			if err != nil {
				fmt.Fprintf(
					os.Stderr,
					"The ConfigurationPolicy at policy-templates index %d has an invalid object-templates array: %v\n",
					i,
					err,
				)
				os.Exit(1)
			}

			for _, objTemplate := range objTemplates {
				jsonBytes, err := json.Marshal(objTemplate)
				if err != nil {
					fmt.Fprintf(
						os.Stderr,
						"The ConfigurationPolicy at policy-templates index %d has an invalid object-templates "+
							"array: %v\n",
						i,
						err,
					)
					os.Exit(1)
				}

				rawDataList = append(rawDataList, jsonBytes)
			}
		}

		objectTemplates := make([]interface{}, 0, len(rawDataList))

		for _, rawData := range rawDataList {
			if bytes.Contains(rawData, []byte("{{hub")) {
				fmt.Fprintf(
					os.Stderr,
					"The ConfigurationPolicy at policy-templates index %d has an unresolved hub template. Use the "+
						"-hub-kubeconfig argument.\n",
					i,
				)
				os.Exit(1)
			}

			tmplResult, err := resolver.ResolveTemplate(rawData, nil, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to process the templates at policy-templates index %d: %v\n", i, err)
				os.Exit(1)
			}

			var resolvedOT interface{}

			err = json.Unmarshal(tmplResult.ResolvedJSON, &resolvedOT)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to process the templates at policy-templates index %d: %v\n", i, err)
				os.Exit(1)
			}

			if oTRawFound {
				switch v := resolvedOT.(type) {
				case []interface{}:
					objectTemplates = v
				case nil:
					objectTemplates = []interface{}{}
				default:
					fmt.Fprintf(
						os.Stderr,
						"object-templates-raw in policy-templates index %d was not an array after templates were "+
							"resolved\n",
						i,
					)
					os.Exit(1)
				}

				unstructured.RemoveNestedField(objectDefinition, "spec", "object-templates-raw")

				break
			}

			objectTemplates = append(objectTemplates, resolvedOT)
		}

		err = unstructured.SetNestedSlice(objectDefinition, objectTemplates, "spec", "object-templates")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to process the templates at policy-templates index %d: %v\n", i, err)
			os.Exit(1)
		}
	}

	err = unstructured.SetNestedSlice(policy.Object, policyTemplates, "spec", "policy-templates")
	if err != nil {
		fmt.Fprintf(os.Stderr, "The resulting policy-templates were invalid: %v\n", err)
		os.Exit(1)
	}

	resolvedPolicy, err := json.Marshal(policy.Object)
	if err != nil {
		fmt.Fprintf(os.Stderr, "The resulting Policy was invalid JSON: %v\n", err)
		os.Exit(1)
	}

	resolvedYAML, err := templates.JSONToYAML(resolvedPolicy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to convert the processed Policy back to YAML: %v\n", err)
		os.Exit(1)
	}

	//nolint: forbidigo
	fmt.Println(string(resolvedYAML))
}
