# MCP Runtime Repository Documentation

This folder documents each source file in the repo, walking line by line (or in tightly grouped blocks) to explain what the code is doing. Use the per-file docs below:

- `docs/cmd-mcp-runtime.md` — CLI entrypoint (`cmd/mcp-runtime`)
- `docs/cmd-operator.md` — Operator entrypoint (`cmd/operator`) and controller internals
- `docs/internal-cli.md` — CLI command implementations under `internal/cli`
- `docs/pkg-metadata.md` — Metadata parsing and CRD generation helpers
- `docs/api.md` — CRD type definitions under `api/v1alpha1`
- `docs/config-and-examples.md` — Kustomize manifests, example apps, and supporting scripts
- `docs/tests.md` — Notes on tests and golden fixtures

Line references use the repository versions at the time of writing; they are 1-based and map directly to the files in the root of the repo.
