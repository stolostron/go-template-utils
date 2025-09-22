// Copyright Contributors to the Open Cluster Management project

package templates

import "testing"

func TestFromClusterClaimInvalidInput(t *testing.T) {
	resolver, err := NewResolver(k8sConfig, Config{})
	if err != nil {
		t.Fatal(err)
	}

	rv, err := resolver.fromClusterClaim(nil, "")
	if err == nil || err.Error() != "a claim name must be provided" {
		t.Fatalf("Expected an error for the missing claim name but got %v", err)
	}

	if rv != "" {
		t.Fatalf("Expected no return value due to the error but got %v", rv)
	}
}

func TestFromClusterClaimNotFound(t *testing.T) {
	resolver, err := NewResolver(k8sConfig, Config{})
	if err != nil {
		t.Fatal(err)
	}

	rv, err := resolver.fromClusterClaim(&ResolveOptions{}, "something-nonexistent")

	expectedMsg := `clusterclaims.cluster.open-cluster-management.io "something-nonexistent" not found`
	if err == nil || err.Error() != expectedMsg {
		t.Fatalf("Expected an error for the missing claim name but got %v", err)
	}

	if rv != "" {
		t.Fatalf("Expected no return value due to the error but got %v", rv)
	}
}
