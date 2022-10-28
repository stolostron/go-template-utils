// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

const clusterClaimAPIVersion string = "cluster.open-cluster-management.io/v1alpha1"

// retrieve the Spec value for the given clusterclaim.
func (t *TemplateResolver) fromClusterClaim(claimname string) (string, error) {
	if t.config.LookupNamespace != "" {
		msg := fmt.Sprintf(
			"fromClusterClaim is not supported because lookups are restricted to the %s namespace",
			t.config.LookupNamespace,
		)

		return "", errors.New(msg)
	}

	dclient, dclientErr := t.getDynamicClient(clusterClaimAPIVersion, "ClusterClaim", "")
	if dclientErr != nil {
		err := fmt.Errorf("failed to get the cluster claim %s: %w", claimname, dclientErr)

		return "", err
	}

	getObj, getErr := dclient.Get(context.TODO(), claimname, metav1.GetOptions{})
	if getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			// Add to the referenced objects if the object isn't found since the consumer may want to watch the object
			// and resolve the templates again once it is present.
			t.addToReferencedObjects(clusterClaimAPIVersion, "ClusterClaim", "", claimname)
		}

		klog.Errorf("Error retrieving clusterclaim : %v, %v", claimname, getErr)

		return "", fmt.Errorf("failed to retrieve the cluster claim %s: %w", claimname, getErr)
	}

	result := getObj.UnstructuredContent()

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		klog.Errorf("The clusterclaim %s has an unexpected format", claimname)

		return "", fmt.Errorf("unexpected cluster claim format: %s", claimname)
	}

	var value string

	if _, ok := spec["value"]; ok {
		value = spec["value"].(string)
	}

	t.addToReferencedObjects(clusterClaimAPIVersion, "ClusterClaim", "", claimname)

	return value, nil
}
