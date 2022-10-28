// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

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
	apiversion string, kind string, namespace string, rsrcname string,
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
	dclient, dclientErr := t.getDynamicClient(apiversion, kind, ns)
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
		listObj, lookupErr := dclient.List(context.TODO(), metav1.ListOptions{})
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
	apiversion string, kind string, namespace string,
) (
	dynamic.ResourceInterface, error,
) {
	var dclient dynamic.ResourceInterface

	gvk := schema.FromAPIVersionAndKind(apiversion, kind)

	klog.V(2).Infof("GVK is:  %v", gvk)

	// we have GVK but We need GVR i.e resourcename for kind inorder to create dynamicClient
	// find ApiResource for given GVK
	apiResource, findErr := t.findAPIResource(gvk)
	if findErr != nil {
		return nil, findErr
	}
	// make GVR from ApiResource
	gvr := schema.GroupVersionResource{
		Group:    apiResource.Group,
		Version:  apiResource.Version,
		Resource: apiResource.Name,
	}
	klog.V(2).Infof("GVR is:  %v", gvr)

	// get Dynamic Client
	dclientIntf, dclientErr := dynamic.NewForConfig(t.kubeConfig)
	if dclientErr != nil {
		klog.Errorf("Failed to get dynamic client with err: %v", dclientErr)

		return nil, fmt.Errorf("failed to get the dynamic client: %w", dclientErr)
	}

	// get Dynamic Client for GVR
	dclientNsRes := dclientIntf.Resource(gvr)

	// get Dynamic Client for GVR for Namespace if namespaced
	if apiResource.Namespaced && namespace != "" {
		dclient = dclientNsRes.Namespace(namespace)
	} else {
		dclient = dclientNsRes
	}

	klog.V(2).Infof("dynamic client: %v", dclient)

	return dclient, nil
}

func (t *TemplateResolver) findAPIResource(gvk schema.GroupVersionKind) (metav1.APIResource, error) {
	klog.V(2).Infof("GVK is: %v", gvk)

	apiResource := metav1.APIResource{}

	// check if an apiresource list is available already (i.e provided as input to templates)
	// if not available use api discovery client to get api resource list
	apiResList := t.config.KubeAPIResourceList
	if apiResList == nil {
		var ddErr error
		apiResList, ddErr = t.discoverAPIResources()

		if ddErr != nil {
			return apiResource, fmt.Errorf("")
		}
	}

	// find apiResourcefor given GVK
	var groupVersion string
	if gvk.Group != "" {
		groupVersion = gvk.Group + "/" + gvk.Version
	} else {
		groupVersion = gvk.Version
	}

	klog.V(2).Infof("GroupVersion is: %v", groupVersion)

	found := false

	for _, apiResGroup := range apiResList {
		if apiResGroup.GroupVersion == groupVersion {
			for _, apiRes := range apiResGroup.APIResources {
				if apiRes.Kind == gvk.Kind {
					apiResource = apiRes
					apiResource.Group = gvk.Group
					apiResource.Version = gvk.Version
					found = true

					klog.V(2).Infof("found the APIResource: %v", apiResource)

					break
				}
			}

			// If the kind isn't found in the matching group version, then the kind doesn't exist.
			break
		}
	}

	if !found {
		klog.V(2).Infof("The APIResource for the GVK wasn't found: %v", gvk)

		t.missingAPIResource = true

		return apiResource, ErrMissingAPIResource
	}

	return apiResource, nil
}

// Configpolicycontroller sets the apiresource list on the template processor
// So this func shouldnt  execute in the configpolicy flow
// including this just for completeness.
func (t *TemplateResolver) discoverAPIResources() ([]*metav1.APIResourceList, error) {
	klog.V(2).Infof("discover APIResources")

	dd, ddErr := discovery.NewDiscoveryClientForConfig(t.kubeConfig)
	if ddErr != nil {
		klog.Errorf("Failed to create the discovery client with err: %v", ddErr)

		return nil, fmt.Errorf("failed to create the discovery client: %w", ddErr)
	}

	_, apiresourcelist, apiresourcelistErr := dd.ServerGroupsAndResources()
	if apiresourcelistErr != nil {
		klog.Errorf("Failed to retrieve apiresourcelist with err: %v", apiresourcelistErr)

		return nil, fmt.Errorf("failed to retrieve apiresourcelist: %w", apiresourcelistErr)
	}

	klog.V(2).Infof("discovered APIResources: %v", apiresourcelist)

	return apiresourcelist, nil
}
