// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

func lookup(apiversion string, kind string, namespace string, rsrcname string) (map[string]interface{}, error) {
	glog.V(glogDefLvl).Infof("lookup :  %v, %v, %v, %v", apiversion, kind, namespace, rsrcname)

	result := make(map[string]interface{})

	// get dynamic Client for the given GVK and namespace
	dclient, dclientErr := getDynamicClient(apiversion, kind, namespace)
	if dclientErr != nil {
		return result, dclientErr
	}

	// if resourcename is  set then get the specific resource
	// else get list of all resources for that (gvk, ns)
	var lookupErr error

	if rsrcname != "" {
		getObj, getErr := dclient.Get(context.TODO(), rsrcname, metav1.GetOptions{})
		if getErr == nil {
			result = getObj.UnstructuredContent()
		}

		lookupErr = getErr
	} else {
		listObj, listErr := dclient.List(context.TODO(), metav1.ListOptions{})
		if listErr == nil {
			result = listObj.UnstructuredContent()
		}

		lookupErr = listErr
	}

	if lookupErr != nil {
		if apierrors.IsNotFound(lookupErr) {
			lookupErr = nil
		}
	}

	glog.V(glogDefLvl).Infof("lookup result:  %v", result)

	return result, lookupErr
}

// this func finds the GVR for given GVK and returns a namespaced dynamic client.
func getDynamicClient(apiversion string, kind string, namespace string) (dynamic.ResourceInterface, error) {
	var dclient dynamic.ResourceInterface
	gvk := schema.FromAPIVersionAndKind(apiversion, kind)
	glog.V(glogDefLvl).Infof("GVK is:  %v", gvk)

	// we have GVK but We need GVR i.e resourcename for kind inorder to create dynamicClient
	// find ApiResource for given GVK
	apiResource, findErr := findAPIResource(gvk)
	if findErr != nil {
		return nil, findErr
	}
	// make GVR from ApiResource
	gvr := schema.GroupVersionResource{
		Group:    apiResource.Group,
		Version:  apiResource.Version,
		Resource: apiResource.Name,
	}
	glog.V(glogDefLvl).Infof("GVR is:  %v", gvr)

	// get Dynamic Client
	dclientIntf, dclientErr := dynamic.NewForConfig(kubeConfig)
	if dclientErr != nil {
		glog.Errorf("Failed to get dynamic client with err: %v", dclientErr)

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

	glog.V(glogDefLvl).Infof("dynamic client: %v", dclient)

	return dclient, nil
}

func findAPIResource(gvk schema.GroupVersionKind) (metav1.APIResource, error) {
	glog.V(glogDefLvl).Infof("GVK is: %v", gvk)

	apiResource := metav1.APIResource{}

	// check if an apiresource list is available already (i.e provided as input to templates)
	// if not available use api discovery client to get api resource list
	apiResList := kubeAPIResourceList
	if apiResList == nil {
		var ddErr error
		apiResList, ddErr = discoverAPIResources()

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
	glog.V(glogDefLvl).Infof("GroupVersion is: %v", groupVersion)

	for _, apiResGroup := range apiResList {
		if apiResGroup.GroupVersion == groupVersion {
			for _, apiRes := range apiResGroup.APIResources {
				if apiRes.Kind == gvk.Kind {
					apiResource = apiRes
					apiResource.Group = gvk.Group
					apiResource.Version = gvk.Version

					break
				}
			}
		}
	}

	glog.V(glogDefLvl).Infof("found APIResource :  %v", apiResource)

	return apiResource, nil
}

// Configpolicycontroller sets the apiresource list on the template processor
// So this func shouldnt  execute in the configpolicy flow
// including this just for completeness.
func discoverAPIResources() ([]*metav1.APIResourceList, error) {
	glog.V(glogDefLvl).Infof("discover APIResources")

	dd, ddErr := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if ddErr != nil {
		glog.Errorf("Failed to create the discovery client with err: %v", ddErr)

		return nil, fmt.Errorf("failed to create the discovery client: %w", ddErr)
	}

	apiresourcelist, apiresourcelistErr := dd.ServerResources()
	if apiresourcelistErr != nil {
		glog.Errorf("Failed to retrieve apiresourcelist with err: %v", apiresourcelistErr)

		return nil, fmt.Errorf("failed to retrieve apiresourcelist: %w", apiresourcelistErr)
	}

	glog.V(glogDefLvl).Infof("discovered APIResources: %v", apiresourcelist)

	return apiresourcelist, nil
}
