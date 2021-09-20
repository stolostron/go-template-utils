// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// retrieve the Spec value for the given clusterclaim.
func (t *TemplateResolver) fromClusterClaim(claimname string) (string, error) {
	if t.config.LookupNamespace != "" {
		msg := fmt.Sprintf(
			"fromClusterClaim is not supported because lookups are restricted to the %s namespace",
			t.config.LookupNamespace,
		)

		return "", errors.New(msg)
	}

	dclient, dclientErr := t.getDynamicClient(
		"cluster.open-cluster-management.io/v1alpha1",
		"ClusterClaim",
		"",
	)
	if dclientErr != nil {
		err := fmt.Errorf("failed to get the cluster claim %s: %w", claimname, dclientErr)

		return "", err
	}

	getObj, getErr := dclient.Get(context.TODO(), claimname, metav1.GetOptions{})
	if getErr != nil {
		glog.Errorf("Error retrieving clusterclaim : %v, %v", claimname, getErr)

		return "", fmt.Errorf("failed to retrieve the cluster claim %s: %w", claimname, getErr)
	}

	result := getObj.UnstructuredContent()

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		glog.Errorf("The clusterclaim %s has an unexpected format", claimname)

		return "", fmt.Errorf("unexpected cluster claim format: %s", claimname)
	}

	if _, ok := spec["value"]; ok {
		return spec["value"].(string), nil
	}

	return "", nil
}
