// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func (t *TemplateResolver) getNamespace(funcName, namespace string) (string, error) {
	// When lookupNamespace is an empty string, there are no namespace restrictions.
	if t.config.LookupNamespace != "" {
		// If lookupNamespace is set but namespace is an empty string, then default
		// to lookupNamespace for convenience
		if namespace == "" {
			return t.config.LookupNamespace, nil
		}

		if t.config.LookupNamespace != namespace {
			msg := fmt.Sprintf(
				"the namespace argument passed to %s is restricted to %s",
				funcName,
				t.config.LookupNamespace,
			)
			klog.Error(msg)

			return "", errors.New(msg)
		}
	}

	return namespace, nil
}

func (t *TemplateResolver) lookup(
	apiversion string, kind string, namespace string, rsrcname string, labelselector ...string,
) (
	map[string]interface{}, error,
) {
	klog.V(2).Infof("lookup :  %v, %v, %v, %v", apiversion, kind, namespace, rsrcname)

	result := make(map[string]interface{})

	ns, err := t.getNamespace("lookup", namespace)
	if err != nil {
		return result, err
	}

	// get dynamic Client for the given GVK and namespace
	dclient, dclientErr := t.getDynamicClient(apiversion, kind, ns, rsrcname)
	if dclientErr != nil {
		// Treat a missing API resource as if an object was not found.
		if errors.Is(dclientErr, ErrMissingAPIResource) {
			return result, nil
		}

		return result, dclientErr
	}

	// if resourcename is  set then get the specific resource
	// else get list of all resources for that (gvk, ns)
	var lookupErr error

	if rsrcname != "" {
		getObj, lookupErr := dclient.Get(context.TODO(), rsrcname, metav1.GetOptions{})
		if lookupErr != nil {
			if apierrors.IsNotFound(lookupErr) {
				// Add to the referenced objects if the object isn't found since the consumer may want to watch the
				// object and resolve the templates again once it is present.
				t.addToReferencedObjects(apiversion, kind, ns, rsrcname)
			}
		} else {
			result = getObj.UnstructuredContent()
			t.addToReferencedObjects(apiversion, kind, ns, rsrcname)
		}
	} else {
		listOptions := metav1.ListOptions{}

		// If labelSelector is defined, and is not an empty string, then add the labels to the listOptions
		// Note there can be multiple values passed to labelselector so we need to treat it as an array
		// The ListOption requires a single string value.
		if len(labelselector) > 0 && labelselector[0] != "" {
			// We use the labels.Parse to validate the selector given.
			// this should give us a better error output if the user misconfigured the selector
			parsedSelector, lookupErr := labels.Parse(strings.Join(labelselector, ","))
			if lookupErr != nil {
				return nil, lookupErr
			}

			klog.V(2).Infof("lookup labels:  %v", parsedSelector)
			listOptions = metav1.ListOptions{
				LabelSelector: parsedSelector.String(),
			}
		}
		listObj, lookupErr := dclient.List(context.TODO(), listOptions)
		if lookupErr == nil {
			result = listObj.UnstructuredContent()
			for _, item := range listObj.Items {
				t.addToReferencedObjects(item.GetAPIVersion(), item.GetKind(), ns, item.GetName())
			}
		}
	}

	if lookupErr != nil {
		if apierrors.IsNotFound(lookupErr) {
			lookupErr = nil
		}
	}

	klog.V(2).Infof("lookup result:  %v", result)

	return result, lookupErr
}

// this func finds the GVR for given GVK and returns a namespaced dynamic client.
func (t *TemplateResolver) getDynamicClient(
	apiversion string, kind string, namespace string, name string,
) (
	dynamic.ResourceInterface, error,
) {
	var dclient dynamic.ResourceInterface

	gvk := schema.FromAPIVersionAndKind(apiversion, kind)

	klog.V(2).Infof("GVK is:  %v", gvk)

	// we have GVK but We need GVR i.e resourcename for kind inorder to create dynamicClient
	scopedGVRObj, findErr := t.findAPIResource(gvk)
	if findErr != nil {
		return nil, findErr
	}

	klog.V(2).Infof("GVR is:  %v", scopedGVRObj.GroupVersionResource)

	if !scopedGVRObj.namespaced && t.config.LookupNamespace != "" {
		rsrcIdentifier := ClusterScopedObjectIdentifier{
			Group: scopedGVRObj.Group,
			Kind:  kind,
			Name:  name,
		}
		if !onAllowlist(t.config.ClusterScopedAllowList, rsrcIdentifier) {
			return nil, ClusterScopedLookupRestrictedError{kind, name}
		}
	}

	// get Dynamic Client
	if t.dynamicClient == nil {
		dClient, err := dynamic.NewForConfig(t.kubeConfig)
		if err != nil {
			klog.Errorf("Failed to get the dynamic client with err: %v", err)

			return nil, fmt.Errorf("failed to get the dynamic client: %w", err)
		}

		t.dynamicClient = dClient
	}

	dclientNsRes := t.dynamicClient.Resource(scopedGVRObj.GroupVersionResource)

	// get Dynamic Client for GVR for Namespace if namespaced
	if scopedGVRObj.namespaced && namespace != "" {
		dclient = dclientNsRes.Namespace(namespace)
	} else {
		dclient = dclientNsRes
	}

	return dclient, nil
}

func (t *TemplateResolver) findAPIResource(gvk schema.GroupVersionKind) (*scopedGVR, error) {
	klog.V(2).Infof("GVK is: %v", gvk)

	if gvr, ok := t.gvkToGVR[gvk]; ok {
		if gvr == nil {
			t.missingAPIResource = true
		}

		return gvr, nil
	}

	apiResList := t.config.KubeAPIResourceList
	groupVersion := gvk.GroupVersion().String()

	// Check if the cached resource list was provided
	if apiResList == nil {
		rv, err := (*t.kubeClient).Discovery().ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			t.missingAPIResource = true

			if apierrors.IsNotFound(err) {
				klog.Infof("The group version was not found: %s", groupVersion)

				return nil, ErrMissingAPIResource
			}

			return nil, err
		}

		apiResList = []*metav1.APIResourceList{rv}
	}

	for _, apiResGroup := range apiResList {
		if apiResGroup.GroupVersion == groupVersion {
			for _, apiRes := range apiResGroup.APIResources {
				if apiRes.Kind == gvk.Kind {
					klog.V(2).Infof("Found the APIResource: %v", apiRes)

					gv := gvk.GroupVersion()

					t.gvkToGVR[gvk] = &scopedGVR{
						GroupVersionResource: schema.GroupVersionResource{
							Group:    gv.Group,
							Version:  gv.Version,
							Resource: apiRes.Name,
						},
						namespaced: apiRes.Namespaced,
					}

					return t.gvkToGVR[gvk], nil
				}
			}

			// If the kind isn't found in the matching group version, then the kind doesn't exist.
			break
		}
	}

	klog.V(2).Infof("The APIResource for the GVK wasn't found: %v", gvk)

	t.missingAPIResource = true
	t.gvkToGVR[gvk] = nil

	return nil, ErrMissingAPIResource
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
