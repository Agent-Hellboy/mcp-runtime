# MCP Runtime Agent Skills

Last checked: 2026-05-18.

This directory contains MCP Runtime's repo-local Agent Skills. The format is
based on the public Agent Skills guidance:

- Overview: https://agentskills.io/home
- Specification: https://agentskills.io/specification
- Best practices: https://agentskills.io/skill-creation/best-practices
- Evaluating skills: https://agentskills.io/skill-creation/evaluating-skills
- Using scripts: https://agentskills.io/skill-creation/using-scripts

## Layout

Each skill lives in its own directory and must contain `SKILL.md`.

```text
.codex/skills/
  <skill-name>/
    SKILL.md
    agents/openai.yaml
    evals/evals.json
    evals/trigger_queries.json
    references/        # optional, loaded only when needed
    scripts/           # optional, reusable helpers owned by that skill
  scripts/
    validate_skill_evals.py
```

`agents/openai.yaml` is client-specific metadata for OpenAI/Codex surfaces. It
is intentionally outside the core Agent Skills spec, which requires only
`SKILL.md` with `name` and `description` frontmatter.

## Agent Skills Compliance Snapshot

The current repo-local skills follow the core Agent Skills structure:

- 11 skills have a `SKILL.md`.
- Every skill `name` matches its directory name.
- Every skill name uses lowercase letters, numbers, and hyphens only.
- Every `description` is non-empty and below the 1024-character limit.
- Every main `SKILL.md` is below the recommended 500-line ceiling.
- Every skill now has a minimal `evals/evals.json` suite with realistic prompts,
  expected outputs, and objective assertions.
- Every skill also has `evals/trigger_queries.json` with should-trigger and
  should-not-trigger prompts for description tuning.
- `.codex/skills/scripts/validate_skill_evals.py` validates every eval and
  trigger manifest so malformed regression prompts fail fast.
- MCP live transport details are split into
  `mcp-spec-compliance/references/live-conformance.md`.
- Large UI coverage detail is split into
  `qa-e2e-ui/references/ui-coverage.md` for progressive disclosure.
- Shared report templates are referenced through relative paths such as
  `../_shared/FINDINGS-TEMPLATE.md`.

Current line counts:

```text
237  k8s-hardening-audit/SKILL.md
358  mcp-spec-compliance/SKILL.md
279  qa-cluster-bringup/SKILL.md
221  qa-e2e-operations/SKILL.md
253  qa-e2e-perf/SKILL.md
329  qa-e2e-security/SKILL.md
283  qa-e2e-ui/SKILL.md
 59  repo-guidance-sync/SKILL.md
265  security-audit-platform/SKILL.md
100  security-audit/SKILL.md
200  supply-chain-audit/SKILL.md
139  mcp-spec-compliance/references/live-conformance.md
308  qa-e2e-ui/references/ui-coverage.md
```

Current gap against the fuller Agent Skills evaluation guidance: the skills now
have output and trigger prompt suites plus a local manifest validator, but there
is not yet a model-backed runner that executes each case with-skill versus
baseline and aggregates pass rates, timing, trigger rates, or token costs. Today
we validate skill/eval structure and run MCP Runtime product regression suites
that the skills instruct agents to run.

## Regression Timing

These timings were measured on 2026-05-18 from the repo root on an existing
`kind-mcp-runtime` contributor cluster.

| Check | Command or flow | Time observed | What it covers |
|---|---|---:|---|
| Skill frontmatter validation | `quick_validate.py` over all skill dirs | 0.59s | Required `SKILL.md` metadata and basic structure |
| Skill eval JSON validation | `python -m json.tool` over `*/evals/*.json` | 0.72s | Output and trigger eval prompt suites parse as JSON for all 11 skills |
| Skill eval schema validation | `.codex/skills/scripts/validate_skill_evals.py` | 0.08s | Eval IDs, prompts, assertions, trigger positives, and trigger negatives for all 11 skills |
| OpenAI skill metadata smoke | shell check over `agents/openai.yaml` | 0.13s | Client metadata files exist with display name, short description, and default prompt |
| UI service unit tests | `cd services/ui && go test ./... -race -count=1` | 6.99s | UI server handlers, config, auth/session behavior covered by Go tests |
| CLI golden tests | `go test ./test/golden/... -count=1` | 4.64s | CLI help/output drift that can affect docs and UI-adjacent flows |
| UI static syntax | `node --check services/ui/static/app.js` | 0.30s | Browser bundle JavaScript parses |
| UI skill validation | `quick_validate.py .codex/skills/qa-e2e-ui` | 0.07s | `qa-e2e-ui` format after edits |
| UI browser smoke | Playwright against `http://localhost:18080/` | about 4-6 min manual | Signed-out state, tenant login, admin login, tabs, UI-triggered API 200s, console sanity, and mobile overflow |
| Cached Kind e2e smoke/governance | `E2E_CACHE_MODE=1 E2E_SCENARIOS=smoke-auth,governance CLUSTER_NAME=mcp-runtime E2E_KEEP_CLUSTER=1 bash test/e2e/kind.sh` | 630.62s, about 10m31s | Real cluster auth, grant/session governance, gateway policy, CLI flows, ingress, registry auth |

The cached Kind e2e command initially failed after 1m17s because a manual
port-forward was already using `localhost:18080`. That was an environment
collision from the QA session, not a product regression. The port-forward was
stopped and the command was rerun.

## How Skill-Based Regression Works Today

The QA skills are run as guided workflows, not as one monolithic test binary.
The agent activates the relevant skill, reads its `SKILL.md`, and then runs the
checks that match the requested scope:

1. `qa-cluster-bringup` creates or recovers the real Kind test-mode cluster.
2. `qa-e2e-ui` uses browser tooling first, then supports findings with curl,
   static asset checks, UI Go tests, golden tests, and optional cached Kind e2e.
3. `qa-e2e-security`, `qa-e2e-operations`, `qa-e2e-perf`, and
   `mcp-spec-compliance` cover runtime security, operations, performance, and
   protocol-specific regressions against the same live cluster.
4. Audit skills use `../_shared/FINDINGS-TEMPLATE.md` so findings stay
   comparable across security, supply chain, Kubernetes, and protocol reviews.
5. `repo-guidance-sync` checks whether code or behavior changes require docs,
   AGENTS.md, runbook, or contributor guidance updates.
6. `.codex/skills/scripts/validate_skill_evals.py` checks that every skill's
   eval and trigger prompt manifests stay structurally usable.

The live QA skills now carry an explicit regression evidence contract: a pass
requires live cluster/browser/API evidence for the relevant surface, including
negative or denied cases where the feature has auth, policy, protocol, or
role-gating behavior. If the live path cannot run, the result is **blocked**,
not passed by substituting static checks.

For `qa-e2e-ui`, the current smoke coverage includes role-based navigation,
auth flows, key tabs, network/API evidence, console evidence, static assets,
and responsive sanity. Full UI coverage is broader and lives in
`qa-e2e-ui/references/ui-coverage.md`; it includes forms, filters, destructive
actions, empty/error states, and public-host defenses. Destructive UI actions
must use temporary `qa-audit-*` objects only.

## Gaps To Close

- Record with-skill versus baseline results, timing, and pass rates in an eval
  workspace, following the Agent Skills evaluation guidance.
- Add a model-backed trigger-eval runner that measures trigger rates over
  `evals/trigger_queries.json` for each skill.
- Script the UI browser smoke so the 4-6 minute manual pass becomes repeatable
  and produces durable evidence.
- Keep future reusable helpers in `scripts/`, make them non-interactive, add
  `--help`, use structured output, and pin dependencies.
