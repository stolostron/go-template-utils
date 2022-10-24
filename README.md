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

Additionally, the following custom functions are supported:

- `atoi` parses an input string and returns an integer like the
  [Atoi](https://pkg.go.dev/strconv#Atoi) function. For example,
  `{{ "6" | atoi }}`.
- `autoindent` will automatically indent the input string based on the leading
  spaces. For example, `{{ "Templating\nrocks!" | autoindent }}`.
- `base64enc` decodes the input Base64 string to its decoded form. For example,
  `{{ "VGVtcGxhdGVzIHJvY2shCg==" | base64dec }}`.
- `base64enc` encodes an input string in the Base64 format. For example,
  `{{ "Templating rocks!" | base64enc }}`.
- `indent` will indent the input string by specified amount. For example,
  `{{ "Templating\nrocks!" | indent 4 }}`.
- `fromClusterClaim` returns the value of a specific `ClusterClaim`. For
  example, `{{ fromClusterClaim "name" }}`.
- `fromConfigMap` returns the value of a key inside a `ConfigMap`. For example,
  `{{ fromConfigMap "namespace" "config-map-name" "key" }}`.
- `fromSecret` returns the value of a key inside a `Secret`. For example,
  `{{ fromSecret "namespace" "secret-name" "key" }}`. If the `EncryptionMode` is
  set to `EncryptionEnabled`, this will return an encrypted value.
- `lookup` is a generic lookup function for any Kubernetes object. For example,
  `{{ (lookup "v1" "Secret" "namespace" "name").Data.key }}`.
- `protect` is a function that encrypts any string using AES-CBC.
- `toBool` - parses an input boolean string converts it to a boolean but also
  removes any quotes around the map value. For example,
  `key: "{{ "true" | toBool }}"` => `key: true`.
- `toInt` parses an input string and returns an integer but also removes any
  quotes around the map value. For example, `key: "{{ "6" | toInt }}"` =>
  `key: 6`.
- `toLiteral` removes any quotes around the template string after it is
  processed. For example, `key: "{{ "[10.10.10.10, 1.1.1.1]" | toLiteral }}` =>
  `key: [10.10.10.10, 1.1.1.1]`. A good use-case for this is when a `ConfigMap`
  field contains a JSON string that you want to literally replace the template
  with and have it treated as the underlying JSON type.

## CLI (Experimental)

The client CLI tool is used to help during policy development involving
templates. Note that the generated output is only partially validated for
syntax.

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

go run experimental/client.go policy-example.yaml
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

go run experimental/client.go -hub-kubeconfig ~/.kube/config -cluster-name local-cluster policy-example.yaml
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
