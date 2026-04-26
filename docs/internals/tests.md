# Tests

- `internal/operator/controller_test.go`: unit-level reconciling behavior checks using fake client to ensure defaults and readiness updates behave as expected.
- `internal/cli/server_test.go`, `registry_test.go`, and `output_test.go`: validate CLI helpers, especially output formatting and registry push logic, often using mocked commands.
- `test/operator_controller_test.go`: higher-level controller assertions against a test environment.
- `pkg/metadata/loader_test.go`: verifies metadata file loading/defaulting for valid and invalid YAML samples under `pkg/metadata/testdata/`.
- `test/golden/cli/cli_golden_test.go`: runs CLI commands against golden output files under `test/golden/cli/testdata/*.golden` to prevent regressions.
- `test/e2e` scripts (`e2e-kind.sh`, `run-in-docker.sh`, `test/e2e/Dockerfile`) outline end-to-end setup flows for CI, building images and running setup against kind.
