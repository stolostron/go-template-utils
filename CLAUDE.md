# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`go-template-utils` is a Go library for processing Go templates in policy templates and other Open Cluster Management objects. It wraps Go's `text/template` package and provides custom template functions for Kubernetes resource lookups, encryption, and string manipulation.

The library includes:
- **Core template resolver** (`pkg/templates/`) - Main template processing engine with caching support
- **CLI tool** (`cmd/template-resolver/`) - Beta CLI for policy development and template testing
- **Custom template functions** - Kubernetes-specific functions like `lookup`, `fromConfigMap`, `fromSecret`, etc.
- **Linting** (`pkg/templates/lint.go`) - Template linting with rules for trailing whitespace, mismatched delimiters, and unquoted template values

## Build and Test Commands

### Building
```bash
# Build the template-resolver CLI
go build -o bin/template-resolver ./cmd/template-resolver

# Install the CLI tool
go install github.com/stolostron/go-template-utils/v7/cmd/template-resolver@latest
```

### Testing
```bash
# Run all tests with coverage
make test

# Run tests with coverage report
make test-coverage

# Run a single test
KUBEBUILDER_ASSETS="$(shell bin/setup-envtest use 1.28.x -p path)" go test -v -run TestFunctionName ./pkg/templates/

# Run tests for a specific package
make test TESTARGS="-v -run TestFunctionName ./pkg/templates/"
```

### Linting and Formatting
```bash
# Format code (runs gofmt, gofumpt, and gci)
make fmt

# Run all linters
make lint

# Security scan
make gosec-scan
```

## Architecture

### Template Resolution Flow

The core architecture centers around the `TemplateResolver` struct which can operate in two modes:

1. **Non-caching mode** (`NewResolver`): Creates a temporary cache per `ResolveTemplate` call. Suitable for one-off template processing.

2. **Caching mode** (`NewResolverWithCaching` or `NewResolverWithDynamicWatcher`): Maintains persistent watches on Kubernetes resources and caches results. Triggers reconciles when watched objects change via controller-runtime integration.

Key flow:
```
Input (JSON/YAML) → Validation → Template Parsing → Function Execution → YAML/JSON Output
                                        ↓
                              Kubernetes API Queries (cached or direct)
```

### Template Function Categories

1. **Kubernetes Resource Functions** (`k8sresource_funcs.go`):
   - `fromConfigMap`, `copyConfigMapData` - ConfigMap data retrieval
   - `fromSecret`, `copySecretData` - Secret data retrieval (with encryption support)
   - `lookup` - Generic Kubernetes object query

2. **Cluster Configuration Functions** (`clusterconfig_funcs.go`):
   - `fromClusterClaim` - ClusterClaim value retrieval
   - `getNodesWithExactRoles`, `hasNodesWithExactRoles` - Node role queries

3. **Encryption Functions** (`encryption.go`):
   - `protect` - AES-CBC encryption
   - Automatic decryption of `$ocm_encrypted:` prefixed values

4. **String Manipulation Functions**:
   - `autoindent`, `indent` - Dynamic and static indentation
   - `base64enc`, `base64dec` - Base64 encoding/decoding
   - `toInt`, `toBool`, `toLiteral` - Type conversion with quote removal
   - Subset of Sprig functions (see `sprig_wrapper.go` line 14)

### Caching and Dependency Watching

When caching is enabled (`dynamicWatcher` is set):
- The resolver uses `kubernetes-dependency-watches` to create API watches
- Query batches track which resources are accessed during template resolution
- Stale watches are automatically cleaned up when templates no longer reference resources
- The `Watcher` field in `ResolveOptions` identifies the object containing templates

### Encryption Model

The library supports AES-CBC encryption with:
- Primary AES key and optional fallback key
- 128-bit initialization vector (IV)
- Two modes: `EncryptionEnabled` (allows `protect` function) and `DecryptionEnabled` (auto-decrypts)
- Encrypted values use the `$ocm_encrypted:` prefix
- Concurrent decryption support via `DecryptionConcurrency`

## Template Linter

The template linter (`pkg/templates/lint.go`) implements three rules:

1. **trailingWhitespace**: Detects trailing spaces/tabs on non-empty, non-comment lines
2. **mismatchedDelimiters**: Ensures `{{`/`}}` and `{{hub`/`hub}}` delimiters are properly paired
3. **unquotedTemplateValues**: Enforces single-quote wrapping around template expressions in YAML

The linter is integrated into the `template-resolver` CLI via the `--lint` flag. This is a tech preview feature that can be overridden with `ENABLE_LINTING_TECH_PREVIEW=true`.

## Testing Patterns

Tests use Ginkgo/Gomega and require kubebuilder's `envtest`:

```go
// Example test structure
var _ = Describe("TemplateFunction", func() {
    It("should resolve template correctly", func() {
        tmpl := []byte(`key: '{{ "value" }}'`)
        result, err := resolver.ResolveTemplate(tmpl, nil, &templates.ResolveOptions{})
        Expect(err).ToNot(HaveOccurred())
        // Assertions...
    })
})
```

When writing tests:
- Always use single quotes around template expressions in YAML: `'{{ ... }}'`
- Mock Kubernetes resources using `fake.NewSimpleDynamicClient()` or envtest
- Test both success and error paths, especially for encryption/decryption
- Use `InputIsYAML: true` when passing raw YAML to avoid JSON conversion

## Common Patterns

### Hub Template Delimiters

Open Cluster Management supports "hub templates" with special delimiters:
- Managed cluster templates: `{{ ... }}`
- Hub cluster templates: `{{hub ... hub}}`

These are processed at different locations in the policy distribution pipeline.

### Context Transformers

`ContextTransformers` allow modifying the template context using the caching API:

```go
transformer := func(queryAPI CachingQueryAPI, context interface{}) (interface{}, error) {
    // Query additional resources and add to context
    obj, err := queryAPI.Get(gvk, namespace, name)
    // Transform context...
    return transformedContext, nil
}
```

This enables dynamic context enrichment while maintaining proper watch management.

### Namespace Restrictions

Set `LookupNamespace` in `ResolveOptions` to restrict template functions to a single namespace. Use `ClusterScopedAllowList` to permit specific cluster-scoped resources (supports wildcards).

## Contributing Requirements

Before submitting a PR, run:
```bash
make fmt
make lint
make test
```

All commits must be signed off (DCO):
```bash
git commit --signoff -m "commit message"
```
