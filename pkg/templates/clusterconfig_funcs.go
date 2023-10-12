// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"errors"
	"fmt"
)

const clusterClaimAPIVersion string = "cluster.open-cluster-management.io/v1alpha1"

func (t *TemplateResolver) fromClusterClaimHelper(options *ResolveOptions) func(string) (string, error) {
	return func(claimName string) (string, error) {
		return t.fromClusterClaim(options, claimName)
	}
}

// retrieve the Spec value for the given clusterclaim.
func (t *TemplateResolver) fromClusterClaim(options *ResolveOptions, claimName string) (string, error) {
	if claimName == "" {
		return "", errors.New("a claim name must be provided")
	}

	clusterClaim, err := t.getOrList(options, clusterClaimAPIVersion, "ClusterClaim", "", claimName)
	if err != nil {
		return "", err
	}

	spec, ok := clusterClaim["spec"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected ClusterClaim format: %s", claimName)
	}

	var value string

	if _, ok := spec["value"]; ok {
		value = spec["value"].(string)
	}

	return value, nil
}
