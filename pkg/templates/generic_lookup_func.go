// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stolostron/kubernetes-dependency-watches/client"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

type ClusterScopedLookupRestrictedError struct {
	kind string
	name string
}

func (e ClusterScopedLookupRestrictedError) Error() string {
	return fmt.Sprintf("lookup of cluster-scoped resource '%v/%v' is not allowed", e.kind, e.name)
}

// getNamespace checks that the target namespace is allowed based on the configured
// lookupNamespace. If it's not, an error is returned. It then returns the namespace
// that should be used. If the target namespace is not set and the lookupNamespace
// configuration is, then the namespace of lookupNamespace is returned for convenience.
func (t *TemplateResolver) getNamespace(namespace string, lookupNamespace string) (string, error) {
	// When lookupNamespace is an empty string, there are no namespace restrictions.
	if lookupNamespace != "" {
		// If lookupNamespace is set but namespace is an empty string, then default
		// to lookupNamespace for convenience
		if namespace == "" {
			return lookupNamespace, nil
		}

		if lookupNamespace != namespace {
			return "", fmt.Errorf("%w to %s", ErrRestrictedNamespace, lookupNamespace)
		}
	}

	return namespace, nil
}

func (t *TemplateResolver) getOrList(
	options *ResolveOptions,
	templateResult *TemplateResult,
	apiVersion string,
	kind string,
	namespace string,
	name string,
	labelSelector ...string,
) (
	map[string]interface{}, error,
) {
	if options == nil {
		options = &ResolveOptions{}
	}

	if apiVersion == "" || kind == "" {
		return nil, errors.New("the apiVersion and kind are required")
	}

	ns, err := t.getNamespace(namespace, options.LookupNamespace)
	if err != nil {
		return nil, err
	}
	
	// Search through all local resources to see if there is a match before
	// attempting to fetch the resource from the remote clusters
	if len(t.localResources) > 0 {
		localResults := unstructured.UnstructuredList{}
		// Fetch it as List
		if name == "" {
			for _, obj := range t.localResources {
				if obj.GetAPIVersion() == apiVersion && obj.GetKind() == kind && obj.GetNamespace() == namespace {
					t.appendUsedResources(obj, false)
					localResults.Items = append(localResults.Items, obj)
				}
			}
			if len(localResults.Items) > 0 {
				return localResults.UnstructuredContent(), nil
			}
		// Else as a Get
		} else {
			for _, obj := range t.localResources {
				if obj.GetAPIVersion() == apiVersion && obj.GetKind() == kind && obj.GetNamespace() == namespace && obj.GetName() == name {
					t.appendUsedResources(obj, false)
					return obj.UnstructuredContent(), nil
				}
			}
		}
	}


	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, err
	}

	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    kind,
	}

	parsedSelector := labels.NewSelector()
	// If labelSelector is defined, and is not an empty string, then add the labels to the listOptions
	// Note there can be multiple values passed to labelSelector so we need to treat it as an array
	// The ListOption requires a single string value.
	if len(labelSelector) > 0 && labelSelector[0] != "" {
		// We use the labels.Parse to validate the selector given.
		// this should give us a better error output if the user misconfigured the selector
		parsedSelector, err = labels.Parse(strings.Join(labelSelector, ","))
		if err != nil {
			return nil, err
		}
	}

	var scopedGVRObj client.ScopedGVR
	if t.dynamicWatcher != nil {
		scopedGVRObj, err = t.dynamicWatcher.GVKToGVR(gvk)
	} else {
		scopedGVRObj, err = t.tempCallCache.GVKToGVR(gvk)
		if errors.Is(err, client.ErrResourceUnwatchable) {
			// ignoring this error when not using a dynamic watcher
			err = nil
		}
	}

	if err != nil {
		if errors.Is(err, client.ErrNoVersionedResource) {
			return nil, ErrMissingAPIResource
		}

		return nil, err
	}

	if !scopedGVRObj.Namespaced && options.LookupNamespace != "" {
		rsrcIdentifier := ClusterScopedObjectIdentifier{
			Group: scopedGVRObj.Group,
			Kind:  kind,
			Name:  name,
		}
		if !onAllowlist(options.ClusterScopedAllowList, rsrcIdentifier) {
			return nil, ClusterScopedLookupRestrictedError{kind, name}
		}

		// If the namespace is restricted but this is a cluster scoped resource, unset the namespace.
		ns = ""
	}

	if t.dynamicWatcher != nil {
		if name == "" {
			result, err := t.dynamicWatcher.List(*options.Watcher, gvk, ns, parsedSelector)
			if err != nil {
				return nil, err
			}

			resultList := unstructured.UnstructuredList{Items: result}

			if templateResult != nil && kind == "Secret" && len(resultList.Items) > 0 {
				templateResult.HasSensitiveData = true
			}

			return resultList.UnstructuredContent(), nil
		}

		result, err := t.dynamicWatcher.Get(*options.Watcher, gvk, ns, name)
		if err != nil {
			return nil, err
		}

		if result == nil {
			return nil, apierrors.NewNotFound(scopedGVRObj.GroupResource(), name)
		}

		if templateResult != nil && kind == "Secret" {
			templateResult.HasSensitiveData = true
		}

		return result.UnstructuredContent(), nil
	}

	// The dynamic watcher is not used, so use the temporary call cache
	lookupID := client.ObjectIdentifier{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: ns,
		Name:      name,
		Selector:  parsedSelector.String(),
	}

	cachedResults, err := t.tempCallCache.FromObjectIdentifier(lookupID)
	if err != nil {
		if !errors.Is(err, client.ErrNoCacheEntry) {
			return nil, err
		}
	} else {
		// Check if this is a Get or List query
		if name != "" {
			if len(cachedResults) > 0 {
				return cachedResults[0].UnstructuredContent(), nil
			}

			return nil, nil
		}

		resultList := unstructured.UnstructuredList{Items: cachedResults}

		return resultList.UnstructuredContent(), nil
	}

	// It's not cached so it must be retrieved using the dynamic client and then cached

	var dynamciClientRes dynamic.ResourceInterface

	if scopedGVRObj.Namespaced && ns != "" {
		dynamciClientRes = t.dynamicClient.Resource(scopedGVRObj.GroupVersionResource).Namespace(ns)
	} else {
		dynamciClientRes = t.dynamicClient.Resource(scopedGVRObj.GroupVersionResource)
	}

	if name == "" {
		resultUnstructuredList, err := dynamciClientRes.List(
			context.TODO(), metav1.ListOptions{LabelSelector: parsedSelector.String()},
		)
		if err != nil {
			return nil, err
		}

		t.tempCallCache.CacheFromObjectIdentifier(lookupID, resultUnstructuredList.Items)

		// Strip out the other metadata to match what is returned from the cache
		resultUnstructuredList = &unstructured.UnstructuredList{Items: resultUnstructuredList.Items}

		if templateResult != nil && kind == "Secret" && len(resultUnstructuredList.Items) > 0 {
			templateResult.HasSensitiveData = true
		}

		// Cache a not found result
		for _, i := range resultUnstructuredList.Items {
			t.appendUsedResources(i, true)
		}

		return resultUnstructuredList.UnstructuredContent(), nil
	}

	resultUnstructured, err := dynamciClientRes.Get(context.TODO(), name, metav1.GetOptions{})
	if err == nil {
		t.tempCallCache.CacheFromObjectIdentifier(lookupID, []unstructured.Unstructured{*resultUnstructured})
	}

	if err != nil {
		// Cache a not found result
		if apierrors.IsNotFound(err) {
			t.tempCallCache.CacheFromObjectIdentifier(lookupID, []unstructured.Unstructured{})
		}

		return nil, err
	}

	// Cache a not found result
	t.appendUsedResources(*resultUnstructured, true)

	if templateResult != nil && kind == "Secret" {
		templateResult.HasSensitiveData = true
	}

	return resultUnstructured.UnstructuredContent(), nil
}

func (t *TemplateResolver) lookupHelper(
	options *ResolveOptions,
	templateResult *TemplateResult,
) func(string, string, string, string, ...string) (map[string]interface{}, error) {
	return func(
		apiVersion string,
		kind string,
		namespace string,
		name string,
		labelSelector ...string,
	) (map[string]interface{}, error) {
		return t.lookup(options, templateResult, apiVersion, kind, namespace, name, labelSelector...)
	}
}

func (t *TemplateResolver) lookup(
	options *ResolveOptions,
	templateResult *TemplateResult,
	apiVersion string,
	kind string,
	namespace string,
	name string,
	labelSelector ...string,
) (
	map[string]interface{}, error,
) {
	klog.V(2).Infof("lookup :  %v, %v, %v, %v", apiVersion, kind, namespace, name)

	result, lookupErr := t.getOrList(options, templateResult, apiVersion, kind, namespace, name, labelSelector...)

	// lookups don't fail on errors
	if apierrors.IsNotFound(lookupErr) {
		lookupErr = nil
	}

	klog.V(2).Infof("lookup result:  %v", result)

	return result, lookupErr
}

func onAllowlist(allowlist []ClusterScopedObjectIdentifier, rsrc ClusterScopedObjectIdentifier) bool {
	if len(allowlist) == 0 {
		return false
	}

	for _, item := range allowlist {
		if item.Group != "*" && item.Group != rsrc.Group {
			continue
		}

		if item.Kind != "*" && item.Kind != rsrc.Kind {
			continue
		}

		if item.Name == "*" || item.Name == rsrc.Name {
			return true
		}
	}

	return false
}

func (t *TemplateResolver) getNodesWithExactRolesHelper(
	options *ResolveOptions,
	templateResult *TemplateResult,
) func(...string) (
	map[string]interface{}, error,
) {
	return func(name ...string) (
		map[string]interface{}, error,
	) {
		return t.getNodesWithExactRoles(options, templateResult, name...)
	}
}

// function getNodesWithExactRoles returns a list of all nodes with only the
// roles specified.  Any nodes that include other roles in addition
// to the specified roles are not included.
func (t *TemplateResolver) getNodesWithExactRoles(
	options *ResolveOptions,
	templateResult *TemplateResult,
	name ...string,
) (
	map[string]interface{}, error,
) {
	var searchRoles []string

	result := []unstructured.Unstructured{}

	for _, n := range name {
		if strings.TrimSpace(n) != "" {
			searchRoles = append(searchRoles, "node-role.kubernetes.io/"+n)
		}
	}

	if len(searchRoles) == 0 {
		return nil, fmt.Errorf("%w: at least one name must be specified", ErrInvalidInput)
	}

	nodes, err := t.getOrList(options, templateResult, "v1", "Node", "", "", searchRoles...)
	if err != nil {
		return nil, err
	}

	// append the worker role to the searchRoles list, this will cause the label check
	// below to ignore the worker role when evaluating if the node should be included
	// in the output list of nodes
	searchRoles = append(searchRoles, "node-role.kubernetes.io/worker")

	nodeList := unstructured.UnstructuredList{}
	nodeList.SetUnstructuredContent(nodes)

	// check if the nodes found contain any other role labels
	for _, node := range nodeList.Items {
		isMatchedNode := true

		for k := range node.GetLabels() {
			if strings.HasPrefix(k, "node-role.kubernetes.io") &&
				!slices.Contains(searchRoles, k) {
				isMatchedNode = false

				break
			}
		}

		if isMatchedNode {
			result = append(result, node)
		}
	}

	resultList := unstructured.UnstructuredList{Items: result}

	return resultList.UnstructuredContent(), nil
}

func (t *TemplateResolver) hasNodesWithExactRolesHelper(
	options *ResolveOptions,
) func(...string) (bool, error) {
	return func(name ...string) (
		bool, error,
	) {
		return t.hasNodesWithExactRoles(options, name...)
	}
}

// function hasNodesWithExactRoles returns true if there are any nodes labeled with only the
// specified roles.  Does not include nodes which have additional roles on them.
func (t *TemplateResolver) hasNodesWithExactRoles(
	options *ResolveOptions, name ...string,
) (bool, error) {
	var resolvedResult TemplateResult

	nodes, err := t.getNodesWithExactRoles(options, &resolvedResult, name...)
	if err != nil {
		return false, err
	}

	items, _, _ := unstructured.NestedSlice(nodes, "items")

	return len(items) > 0, nil
}
