# Internals

Source-tree walkthroughs for contributors. These are written as line-referenced tours of the actual code in this repo and are most useful when you are reading `git blame`-adjacent or modifying the implementation.

For platform usage, start with the [user docs](../README.md) instead.

## Files

| File | Covers |
|---|---|
| [`cmd-mcp-runtime.md`](cmd-mcp-runtime.md) | CLI entrypoint (`cmd/mcp-runtime/main.go`). |
| [`cmd-operator.md`](cmd-operator.md) | Operator entrypoint (`cmd/operator/main.go`) and `internal/operator` controller logic. |
| [`internal-cli.md`](internal-cli.md) | Cobra command implementations under `internal/cli/`. |
| [`pkg-metadata.md`](pkg-metadata.md) | Metadata parsing and CRD generation helpers. |
| [`api-types.md`](api-types.md) | CRD type definitions in `api/v1alpha1`. |
| [`config-and-examples.md`](config-and-examples.md) | Kustomize manifests, example apps, supporting scripts. |
| [`tests.md`](tests.md) | Test layout, golden fixtures, e2e flows. |

> Line references match the repository at the time of writing; they are 1-based and may drift after refactors.
