// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestFromClusterClaimNsError(t *testing.T) {
	t.Parallel()

	var kubeClient kubernetes.Interface = fake.NewSimpleClientset()

	kubeConfig := &rest.Config{}
	config := Config{LookupNamespace: "my-policies"}
	resolver, _ := NewResolver(&kubeClient, kubeConfig, config)

	_, err := resolver.fromClusterClaim("clusterID")

	if err == nil {
		t.Fatal("Expecting an error but did not get one")
	}

	expectedMsg := "fromClusterClaim is not supported because lookups are restricted to the my-policies namespace"
	if err.Error() != expectedMsg {
		t.Fatalf(`Expected the error "%s", but got "%s"`, expectedMsg, err.Error())
	}
}
