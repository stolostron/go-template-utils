// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// retrieve the Spec value for the given clusterclaim.
func fromClusterClaim(claimname string) (string, error) {
	dclient, dclientErr := getDynamicClient(
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
