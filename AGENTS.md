# AGENTS.md - steward-release

Guide for AI agents working in this repository. Pair with `CLAUDE.md` (the working agreement and
hook-enforced rules). Keep this file current when the build, layout, or public API changes.

## What this is

Steward release: Helm charts for a cloud-native Steward install (identity, core, workflow,
obligations, audit, delivery, pdf-renderer, collab, ai, ai-operator, reporting, gateway, web-staff, web-admin), alongside the
future appliance. The pinned release manifest and the `steward-migrate` tool named in the design
docs are not in this pass's scope; they're follow-up issues.

**Hub gap:** the scaffolding hub has no Helm/chart ecosystem, so this repo was scaffolded on
the closest fit (`action`) for governance files only, with the tree, conventions and release
sections of `CLAUDE.md` rewritten by hand for a chart repo. A governance sync will restore the
`action/action` text under the layout begin-marker; re-apply the chart-repo version from this
repo's history when that happens, and check whether the gap issue filed upstream for it has closed.

## Using steward-release

An operator runs `helm install steward oci://ghcr.io/steward-grc/charts/steward-release` (once
published) or `helm install steward .` from a checkout, after creating the Secrets each service's
values reference by name. See `docs/install.md`.

## Layout

- `Chart.yaml`, `values.yaml` - the umbrella chart; one alias per Steward service.
- `values-ha-example.yaml` - a documented high-availability install (3-replica Postgres and
  services).
- `charts/_service/` - the one reusable chart every service alias composes (Deployment, Service,
  HPA, PodDisruptionBudget, ServiceAccount with a projected workload-auth token, NetworkPolicy).
- `docs/` - install, upgrade, and service-to-service authentication.

## Build, test, lint

- Lint: `helm lint .` and `helm lint charts/_service`
- Template (render only, no cluster needed): `helm template . -f values.yaml`,
  `helm template . -f values.yaml -f values-ha-example.yaml`
- Validate rendered manifests: pipe the template output to `kubeconform` (pinned, checksum-checked
  in CI; see `.github/workflows/checks.yml`)
- No build step, no generated code, nothing to package-check (no `package.json`/`go.mod` here).

## Logging

Follow the logging rules in `CLAUDE.md`. In short:

- Log generously: entry and exit of significant operations, decisions and branches, retries, state
  changes, external calls (target, duration, outcome), and every error with its context.
- Levels: `trace` for step-by-step detail, `debug` for flow, `info` for lifecycle, `warn` and
  `error` for problems. The environment filters the volume, so err on the side of too much.
- Environments: local dev `trace` with `LOG_FORMAT=console` (never JSON), dev cluster `debug`,
  qa/staging `info`, production `error`. Every cluster environment logs JSON. Set levels through
  `LOG_LEVEL` and `LOG_FORMAT`, never in code; this chart's values default every service to
  `error`/`json`.
- Never log secrets, tokens, or personal data, not even at `trace`. Log an opaque or keyed ID.

## Conventions and gotchas

- See `CLAUDE.md` for the branch/commit/PR rules; they are enforced by the git hooks in
  `.claude/hooks` (run `bash .claude/hooks/install.sh` once per clone).
- Open every PR as a draft. CI skips drafts, so run the full checks locally, push once they pass,
  and mark the PR ready when the work is finished; see CLAUDE.md "CI and Actions minutes".
- A merge into this repo's `main` needs the release-review group's approval on top of green CI;
  merge-on-green alone does not apply to this repo.
- `Bugs5382/helm-postgres-ha` has no published release yet (draft-only `v0.0.1`, no OCI chart
  published). `helm dependency build` fails here until the maintainer publishes it; that's a known,
  reported gap, not a bug in this repo.
- Every service alias is a thin values block over `charts/_service`; resist giving a service its
  own chart unless it genuinely needs a template the generic chart can't express (document why, in
  the PR, if it ever happens).
