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
	"text/template"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/stolostron/go-template-utils/v7/pkg/lint"
	"github.com/stolostron/go-template-utils/v7/pkg/templates"
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

// decode local resources to use them during the template resolver instead of fetching resources from remote clusters.
func decodeLocalResources(localResourcesPath string) ([]unstructured.Unstructured, error) {
	localResources := make([]unstructured.Unstructured, 0)
	if localResourcesPath == "" {
		return localResources, nil
	}

	yamlBytes, err := HandleFile(localResourcesPath)
	if err != nil {
		return nil, fmt.Errorf("failed when processing local resource file: %w", err)
	}

	dec := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(yamlBytes), 4096)

	for {
		obj := unstructured.Unstructured{}

		err := dec.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to decode the local resources: %w", err)
		}

		localResources = append(localResources, obj)
	}

	return localResources, nil
}

func Lint(yamlString string) []lint.LinterRuleViolation {
	return lint.Lint(yamlString)
}

// ProcessTemplate takes a YAML byte array input, unmarshals it to a Policy, ConfigPolicy,
// or object-templates-raw, processes the templates, and marshals it back to YAML,
// returning the resulting byte array. Validation is performed along the way, returning
// an error if any failures are found. It uses the `HubKubeConfigPath`, `HubNamespace` and `ClusterName`
// to establish a dynamic client with the hub to resolve any hub templates it finds.
func (t *TemplateResolver) ProcessTemplate(yamlBytes []byte) ([]byte, error) {
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

	if t.HubKubeConfigPath != "" {
		customSA, _, _ := unstructured.NestedString(policy.Object, "spec", "hubTemplateOptions", "serviceAccountName")

		if customSA == "" {
			if policy.GetKind() == "Policy" {
				// neither specified
				if t.HubNamespace == "" && policy.GetNamespace() == "" {
					return nil, errors.New("a namespace must be specified for hub templates, " +
						"either in the input Policy or as an argument if spec.hubTemplateOptions.serviceAccountName " +
						"is not specified")
				}

				// both specified and don't match
				if t.HubNamespace != "" && policy.GetNamespace() != "" && t.HubNamespace != policy.GetNamespace() {
					return nil, errors.New("the namespace specified in the Policy and the " +
						"hub-namespace argument must match")
				}

				// either t.HubNamespace is already specified, or we'll use the one in the policy
				if policy.GetNamespace() != "" {
					t.HubNamespace = policy.GetNamespace()
				}
			} else if t.HubNamespace == "" {
				// Non-Policy types just always require the argument
				return nil, errors.New("a hub namespace must be provided when a hub kubeconfig is provided " +
					"and spec.hubTemplateOptions.serviceAccountName is not specified")
			}
		}

		hubKubeConfig, err := clientcmd.BuildConfigFromFlags("", t.HubKubeConfigPath)
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

		mc, err := dynamicHubClient.Resource(mcGVR).Get(context.TODO(), t.ClusterName, v1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get the ManagedCluster object for %s: %w", t.ClusterName, err)
		}

		if policy.GetKind() == "Policy" {
			hubTemplateOpts.ctx.PolicyMetadata = map[string]interface{}{
				"annotations": policy.GetAnnotations(),
				"labels":      policy.GetLabels(),
				"name":        policy.GetName(),
				"namespace":   policy.GetNamespace(),
			}
		}

		hubTemplateOpts.ctx.ManagedClusterName = t.ClusterName
		hubTemplateOpts.ctx.ManagedClusterLabels = mc.GetLabels()

		// If a custom service account is provided, assume the hub kubeconfig is for that service account
		if customSA == "" {
			hubTemplateOpts.opts = templates.ResolveOptions{
				ClusterScopedAllowList: []templates.ClusterScopedObjectIdentifier{{
					Group: "cluster.open-cluster-management.io",
					Kind:  "ManagedCluster",
					Name:  t.ClusterName,
				}},
				LookupNamespace: t.HubNamespace,
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

		err = createSaveResourcesOutput(t.SaveHubResources, hubResolver)
		if err != nil {
			return nil, err
		}

		policy.Object = hubResolvedObject
	}

	localResources, err := decodeLocalResources(t.LocalResources)
	if err != nil {
		return nil, err
	}

	resolver, err := templates.NewResolverWithLocalResources(kubeConfig, templates.Config{}, localResources)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate the template resolver: %w", err)
	}

	tempCtx := templates.TemplateContext{
		ObjectNamespace: t.ObjNamespace,
		ObjectName:      t.ObjName,
	}

	switch policy.GetKind() {
	case "Policy":
		err = processPolicyTemplate(&policy, resolver, tempCtx)
	case "ConfigurationPolicy":
		err = processConfigPolicyTemplate(&policy, resolver, tempCtx)
	case "OperatorPolicy":
		_, err = processOperatorPolicyTemplates(policy.Object, resolver, tempCtx)
	default:
		if t.SkipPolicyValidation {
			var resolvedRaw any
			resolvedRaw, err = processRawGoTemplate(string(yamlBytes), resolver, tempCtx)

			if resolved, ok := resolvedRaw.(map[string]interface{}); ok {
				policy.Object = resolved
			} else {
				err = errors.New("failed to cast returned object to map[string]interface{}")
			}
		} else {
			if _, ok := policy.Object["object-templates-raw"]; !t.SkipPolicyValidation && !ok {
				return nil, errors.New("invalid YAML. Supported types: Policy, " +
					"ConfigurationPolicy, OperatorPolicy, object-templates-raw")
			}

			err = processObjTemplatesRaw(&policy, resolver, tempCtx)
		}
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

	err = createSaveResourcesOutput(t.SaveResources, resolver)
	if err != nil {
		return nil, err
	}

	return resolvedYAML, nil
}

func createSaveResourcesOutput(path string, resolver *templates.TemplateResolver) error {
	if path != "" {
		f, err := os.Create(path) //#nosec G304 -- files accessed here are on the user's local system
		if err != nil {
			return err
		}

		defer f.Close()

		usedResources := resolver.GetUsedResources()

		for _, r := range usedResources {
			fmt.Fprintln(f, "---")

			if r.IsRemote {
				fmt.Fprintln(f, "# Resource was remotely fetched")
			} else {
				fmt.Fprintln(f, "# Resource was locally fetched")
			}

			out, err := yaml.Marshal(r.Resource.Object)
			if err != nil {
				return err
			}

			fmt.Fprint(f, string(out))
		}
	}

	return nil
}

// ProcessPolicyTemplate takes the unmarshalled Policy YAML as input and resolves
// all valid ConfigurationPolicy templates specified in the policy-templates field
func processPolicyTemplate(
	policy *unstructured.Unstructured,
	resolver *templates.TemplateResolver,
	tempCtx templates.TemplateContext,
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

			objectDefinition, err = processObjectTemplates(objectDefinition, resolver, tempCtx)
			if err != nil {
				return fmt.Errorf("%w (in policy-templates at index %d)", err, i)
			}
		case "policy.open-cluster-management.io/v1beta1":
			if templateObj.GetKind() != "OperatorPolicy" {
				continue
			}

			objectDefinition, err = processOperatorPolicyTemplates(objectDefinition, resolver, tempCtx)
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
	tempCtx templates.TemplateContext,
) error {
	resolvedPolicy, err := processObjectTemplates(policy.Object, resolver, tempCtx)
	if err != nil {
		return err
	}

	policy.Object = resolvedPolicy

	return nil
}

func processRawGoTemplate(
	input string,
	resolver *templates.TemplateResolver,
	tempCtx templates.TemplateContext,
) (resolved any, err error) {
	resolveOptions := templates.ResolveOptions{InputIsYAML: true}

	if strings.Contains(input, "{{hub") {
		return nil, errors.New("unresolved hub template in YAML input. Use the hub-kubeconfig argument")
	}

	tmplResult, err := resolver.ResolveTemplate([]byte(input), tempCtx, &resolveOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to process the templates: %w", err)
	}

	err = json.Unmarshal(tmplResult.ResolvedJSON, &resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to process the templates: %w", err)
	}

	return resolved, err
}

// processObjTemplatesRaw takes a YAML string representation and resolves the object's managed templates
func processObjTemplatesRaw(
	raw *unstructured.Unstructured,
	resolver *templates.TemplateResolver,
	tempCtx templates.TemplateContext,
) error {
	oTRaw, _, _ := unstructured.NestedString(raw.Object, "object-templates-raw")
	if oTRaw == "" {
		return errors.New("invalid object-templates-raw after resolving hub templates")
	}

	resolved, err := processRawGoTemplate(oTRaw, resolver, tempCtx)
	if err != nil {
		return err
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
	tempCtx templates.TemplateContext,
) (map[string]interface{}, error) {
	_, oTRawFound, _ := unstructured.NestedString(objectDefinition, "spec", "object-templates-raw")
	if oTRawFound {
		policy := unstructured.Unstructured{Object: objectDefinition["spec"].(map[string]interface{})}

		err := processObjTemplatesRaw(&policy, resolver, tempCtx)
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

	resolvedTemplates := []interface{}{}
	resolveOptions := templates.ResolveOptions{
		InputIsYAML: false,
	}

	for i, objTemplate := range objTemplates {
		fieldName := fmt.Sprintf("object-templates[%v]", i)
		skipObject := false
		resolveOptions.CustomFunctions = template.FuncMap{
			"skipObject": func(skips ...any) (empty string, err error) {
				switch len(skips) {
				case 0:
					skipObject = true
				case 1:
					if !skipObject {
						if skip, ok := skips[0].(bool); ok {
							skipObject = skip
						} else {
							err = fmt.Errorf(
								"expected boolean but received '%v'", skips[0])
						}
					}
				default:
					err = fmt.Errorf(
						"expected one optional boolean argument but received %d arguments", len(skips))
				}

				return empty, err
			},
		}

		resolved, err := resolveManagedTemplate(objTemplate, fieldName, resolver, resolveOptions, tempCtx)
		if err != nil {
			return nil, err
		}

		if !skipObject {
			resolvedTemplates = append(resolvedTemplates, resolved)
		}
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
	tempCtx templates.TemplateContext,
) (map[string]interface{}, error) {
	resolveOptions := templates.ResolveOptions{
		InputIsYAML: false,
	}

	opGroup, found, err := unstructured.NestedMap(operatorPolicy, "spec", "operatorGroup")
	if err != nil {
		return nil, fmt.Errorf("invalid operatorGroup: %w", err)
	}

	if found {
		resolved, err := resolveManagedTemplate(opGroup, "operatorGroup", resolver, resolveOptions, tempCtx)
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
		resolved, err := resolveManagedTemplate(sub, "subscription", resolver, resolveOptions, tempCtx)
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

	versions, found, err := unstructured.NestedStringSlice(operatorPolicy, "spec", "versions")
	if err != nil {
		return nil, fmt.Errorf("invalid versions: %w", err)
	}

	if found {
		resolved, err := resolveManagedTemplate(versions, "versions", resolver, resolveOptions, tempCtx)
		if err != nil {
			return nil, err
		}

		resolvedVersions := make([]interface{}, 0, len(resolved.([]interface{})))

		for _, version := range resolved.([]interface{}) {
			trimmedVersion := strings.TrimSpace(version.(string))

			if trimmedVersion != "" {
				resolvedVersions = append(resolvedVersions, trimmedVersion)
			}
		}

		err = unstructured.SetNestedField(operatorPolicy, resolvedVersions, "spec", "versions")
		if err != nil {
			return nil, err
		}
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
	tempCtx templates.TemplateContext,
) (interface{}, error) {
	rawData, err := json.Marshal(field)
	if err != nil {
		return nil, fmt.Errorf("invalid %v: %w", fieldName, err)
	}

	if bytes.Contains(rawData, []byte("{{hub")) {
		return nil, errors.New("unresolved hub template in YAML input. Use the hub-kubeconfig argument")
	}

	tmplResult, err := resolver.ResolveTemplate(rawData, tempCtx, &resolveOptions)
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
