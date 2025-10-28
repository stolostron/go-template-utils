[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## What is go-template-utils?

A library for processing Go templates in policy templates or other Open Cluster
Management objects.

Go to the [Contributing guide](CONTRIBUTING.md) to learn how to get involved.

- The `go-template-utils` repository is part of the `open-cluster-management`
  community. For more information, visit:
  [open-cluster-management.io](https://open-cluster-management.io).

## How do templates work?

To get started, use the
[templates.NewResolver](https://pkg.go.dev/github.com/stolostron/go-template-utils/pkg/templates#NewResolver)
function along with a
[templates.Config](https://pkg.go.dev/github.com/stolostron/go-template-utils/pkg/templates#Config)
instance.

See the
[ResolveTemplate example](https://pkg.go.dev/github.com/stolostron/go-template-utils/pkg/templates#example_TemplateResolver_ResolveTemplate)
for an example of how to use this library.

Under the hood, `go-template-utils` wraps the
[text/template](https://pkg.go.dev/text/template) package. This means that as
long as the input to
[templates.ResolveTemplate](https://pkg.go.dev/github.com/stolostron/go-template-utils/pkg/templates#ResolveTemplate)
can be marshaled to YAML, any of the
[text/template](https://pkg.go.dev/text/template) package features can be used.

A subset of [Sprig](https://masterminds.github.io/sprig/) functions is imported 
into the resolver, listed in [`pkg/templates/sprig_wrapper.go`](pkg/templates/sprig_wrapper.go#L14).

Additionally, the following custom functions are supported:

Function | Description | Example
--- | --- | ---
`atoi` | Parses an input string and returns an integer like the [Atoi](https://pkg.go.dev/strconv#Atoi) function. | `{{ "6" \| atoi }}`
`autoindent` | Automatically indents the input string based on the leading spaces. | `{{ "Templating\nrocks!" \| autoindent }}`
`base64enc` | Decodes the input Base64 string to its decoded form. |`{{ "VGVtcGxhdGVzIHJvY2shCg==" \| base64dec }}`
`base64enc` | Encodes an input string in the Base64 format. | `{{ "Templating rocks!" \| base64enc }}`
`indent` | Indents the input string by the specified amount. | `{{ "Templating\nrocks!" \| indent 4 }}`
`fromClusterClaim` | Returns the value of a specific `ClusterClaim`. Errors if the `ClusterClaim` is not found. | `{{ fromClusterClaim "name" }}`
`lookupClusterClaim` | Returns the value of a specific `ClusterClaim`. Returns an empty string if the `ClusterClaim` is not found. | `{{ lookupClusterClaim "name" }}`
`fromConfigMap` | Returns the value of a key inside a `ConfigMap`. Errors if the `ConfigMap` is not found. | `{{ fromConfigMap "namespace" "config-map-name" "key" }}`
`copyConfigMapData` | Returns the `data` contents of the specified `ConfigMap` | `{{ copyConfigMapData "namespace" "config-map-name" }}`
`fromSecret` | Returns the value of a key inside a `Secret`. If the `EncryptionMode` is set to `EncryptionEnabled`, this will return an encrypted value. Errors if the `Secret` is not found. | `{{ fromSecret "namespace" "secret-name" "key" }}`
`copySecretData` | Returns the `data` contents of the specified `Secret`. If the `EncryptionMode` is set to `EncryptionEnabled`, this will return an encrypted value. | `{{ copySecretData "namespace" "secret-name" }}`
`lookup` | Generic lookup function for any Kubernetes object. Returns an empty string if the resource is not found. | `{{ (lookup "v1" "Secret" "namespace" "name").data.key }}`
`protect` | Encrypts any string using AES-CBC. | `{{ "super-secret" \| protect }}`
`toBool` | Parses an input boolean string converts it to a boolean but also removes any quotes around the map value. | `key: "{{ "true" \| toBool }}"` => `key: true`
`toInt` | Parses an input string and returns an integer but also removes anyquotes around the map value. |  `key: "{{ "6" \| toInt }}"` => `key: 6`
`toLiteral` | Removes any quotes around the template string after it is processed. | `key: "{{ "[10.10.10.10, 1.1.1.1]" \| toLiteral }}` => `key: [10.10.10.10, 1.1.1.1]`
`getNodesWithExactRoles` | Returns a list of nodes with only the role(s) specified, ignores nodes that have any additional roles except "*node-role.kubernetes.io/worker*" role. | `{{ (getNodesWithExactRoles "infra").items }}`
`hasNodesWithExactRoles` | Returns `true` if the cluster contains node(s) with only the role(s) specified, ignores nodes that have any additional roles except "*node-role.kubernetes.io/worker*" role. | `key: {{ (hasNodesWithExactRoles "infra") }}` => `key: true`

## `template-resolver` CLI (Beta)

The `template-resolver` CLI tool is used to help during policy development involving
templates. Note that the generated output is only partially validated for
syntax.

### Installing the binary

```bash
go install github.com/stolostron/go-template-utils/v7/cmd/template-resolver@latest
```

### Managed Cluster Templates Example

```bash
kubectl -n default create configmap cool-car --from-literal=model=Shelby\ Mustang
kubectl -n default create configmap not-cool-car --from-literal=model=Pinto

cat <<EOF > policy-example.yaml
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: label-configmaps
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: label-configmaps
        spec:
          remediationAction: enforce
          severity: low
          object-templates-raw: |
            {{- range (lookup "v1" "ConfigMap" "default" "").items }}
            {{- if and .data.model (contains "Mustang" .data.model) }}
            - complianceType: musthave
              objectDefinition:
                kind: ConfigMap
                apiVersion: v1
                metadata:
                  name: {{ .metadata.name }}
                  namespace: {{ .metadata.namespace }}
                  labels:
                    ford.com/model: Mustang
            {{- end }}
            {{- end }}
EOF

template-resolver policy-example.yaml
```

The output should be:

```yaml
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: label-configmaps
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: label-configmaps
        spec:
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: v1
                kind: ConfigMap
                metadata:
                  labels:
                    ford.com/model: Mustang
                  name: cool-car
                  namespace: default
          remediationAction: enforce
          severity: low
```

### Hub and Managed Cluster Templates Example

```bash
kubectl -n default create configmap cool-car --from-literal=model=Shelby\ Mustang
kubectl -n default create configmap not-cool-car --from-literal=model=Pinto

cat <<EOF > policy-example.yaml
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: label-configmaps
  namespace: policies
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: label-configmaps
        spec:
          remediationAction: enforce
          severity: low
          object-templates-raw: |
            {{- range (lookup "v1" "ConfigMap" "default" "").items }}
            {{- if and .data.model (contains "Mustang" .data.model) }}
            - complianceType: musthave
              objectDefinition:
                kind: ConfigMap
                apiVersion: v1
                metadata:
                  name: {{ .metadata.name }}
                  namespace: {{ .metadata.namespace }}
                  labels:
                    cluster-name: {{hub .ManagedClusterName hub}}
                    ford.com/model: Mustang
            {{- end }}
            {{- end }}
EOF

template-resolver -hub-kubeconfig ~/.kube/config -cluster-name local-cluster policy-example.yaml
```

The output should be:

```yaml
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: label-configmaps
  namespace: policies
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: label-configmaps
        spec:
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: v1
                kind: ConfigMap
                metadata:
                  labels:
                    cluster-name: local-cluster
                    ford.com/model: Mustang
                  name: cool-car
                  namespace: default
          remediationAction: enforce
          severity: low
```
