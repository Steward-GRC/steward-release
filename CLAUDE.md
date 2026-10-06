# CLAUDE.md - steward-release

Working agreement for this repository. The governance below is shared across the org's repos and
kept in sync with them.

## Enforced by hooks (run `bash .claude/hooks/install.sh` once per clone)

- Conventional Commits on commits, issue titles, and PR titles.
- No AI tells in commits/issues/PRs/comments/source; no emoji in source or commit messages (emoji
  are allowed in Markdown docs and CI workflow files).
- Pre-push: the ecosystem's format/lint/test gate must pass (Go: gofmt/vet/golangci-lint/test;
  npm: lint/test scripts; Python: ruff/pytest).

## Conventions

- Branching: never commit to `main`. Work on a feature/working branch; open a PR.
- Commits: Conventional Commits (`type(scope): description`). The operator (@Bugs5382) is the
  author of record on every commit.
- Voice: human-authored. No attribution trailers (`Co-Authored-By`, `Generated with`), no robot
  glyphs/emoji, no session framing.
- Local notes live in the git-ignored `.local/` folder; delete a note when its work is done.
- GitHub Actions: a job id must be a plain identifier (a letter or `_`, then alphanumerics/`-`/`_`);
  put emoji and display text in the job's `name:`, never the job key. The Actionlint check
  (`.github/workflows/action-lint.yaml`) enforces this, so a malformed workflow fails at PR time
  instead of silently at startup on `main`.

<!-- layout:begin action/action -->
## Project layout

**Not an action repo.** The scaffolding hub has no chart/Helm ecosystem yet (a gap filed
upstream), so this repo was scaffolded on the closest-fitting ecosystem (`action`) for its
governance files only (license, NOTICE, CODEOWNERS, CI callers, hooks) and the tree and
conventions below replace that ecosystem's own, by hand, until the hub adds one. A governance sync
will restore the `action/action` text below the begin marker; re-apply this section when that
happens, and check whether the gap issue has closed.

This repo holds the Helm charts, the pinned release manifest and the migration tool for a cloud-native
Steward install, alongside the appliance.

### Tree

```text
.
├── Chart.yaml                 the umbrella chart: one alias per Steward service
├── values.yaml                per-service defaults
├── values-ha-example.yaml     documents a 3-replica Postgres + multi-replica services install
├── charts/
│   ├── _service/               the reusable chart every service alias composes
│   │   ├── templates/          deployment, service, hpa, pdb, serviceaccount, role, networkpolicy
│   │   └── tests/              helm-unittest suites for the reusable chart
│   ├── _pdf-renderer-crds/     the PdfRender CRD, vendored (crds/ only)
│   └── _ai-crds/               the PolicyAIJob CRD, vendored (crds/ only)
├── scripts/                   sync-crds.sh and the CRD pins it reads (crds-upstream.txt)
├── templates/NOTES.txt        the umbrella's install notes (required Postgres extensions)
├── tests/                     helm-unittest suites for the umbrella's per-service wiring
├── docs/                      install, upgrade and service-to-service auth
├── .github/workflows/         this repo's CI: helm lint/template/kubeconform, DCO, secrets
└── README.md                  what this is, how to install, where to look
```

### What goes where

- `Chart.yaml`/`values.yaml`: the only files an operator needs to `helm install`. Every alias is
  `.enabled`-gated; every secret reference names an existing Secret, never a plaintext default.
- `charts/_service/`: the one reusable chart. A new service alias is a values block, never a new
  template.
- No package manifest to publish: a release is a signed, pinned manifest plus a chart version bump,
  cut by hand by the maintainer. Never tagged or published from this box.

### Naming

- Chart aliases are `steward-<service>` names without the `steward-` prefix repeated in values keys
  (`identity:`, not `stewardIdentity:`).
- Values keys are `camelCase`; environment variable names the charts set are `SCREAMING_SNAKE_CASE`
  (what the Go services read).

### Tests, fixtures and generated code

- The only vendored files are the CRDs under `charts/_*-crds/crds/`, copied by
  `scripts/sync-crds.sh` from each service at the commit in `scripts/crds-upstream.txt`. Never edit
  them by hand; bump the pin and re-run the script.
- No generated code here. `helm template` output is never committed; CI renders it fresh from
  `values.yaml`, `values-ha-example.yaml` and a bring-your-own-Postgres example, each piped to a
  pinned, checksum-checked `kubeconform`.
- Template unit tests are helm-unittest suites in `charts/_service/tests/*_test.yaml` (`task test`;
  CI runs the pinned, checksum-checked standalone binary). Every template change gets a case.
<!-- layout:end -->

## CI and Actions minutes

GitHub bills every job for at least one full minute, and a private org's included minutes run out
fast during a wave of PRs. The shipped workflows are shaped around that:

- **Drafts run nothing.** PR workflows skip draft PRs and run on `ready_for_review`, `opened`,
  `synchronize` and `reopened`. Open a PR as a draft, run the full checks locally, push once they
  pass, and mark it ready when the work is finished. That starts one CI run. After it is ready,
  push only real fixes, batched into one push.
- **One job for the small checks.** PR Title, PR Body, PR Hygiene and the gitleaks secret scan are
  steps of one `✅ PR Checks` job (`job-pr-checks.yaml`). Every step runs even when an earlier one
  fails, so the log shows every failure. This job and the label checker are the only workflows
  that react to `edited`: a title or body fix reruns them, not the build.
- **Pull requests only.** Build, test, lint, licence and security workflows run on pull requests,
  not on push to `main`. The squash merge lands the tree the PR run already tested. Only the
  release workflows (Release Manager, Release Drafter, publish) run on `main`, and Go Security keeps
  its weekly schedule for advisories published later.
- **Keep the PR run honest:** the PR run covers the merged code only when the branch is up to date
  with `main` before it merges. In the branch ruleset, add **Require status checks to pass** with
  the repo's check names and turn on **Require branches to be up to date before merging** (API:
  the `required_status_checks` rule with `strict_required_status_checks_policy: true`; classic
  branch protection: `required_status_checks.strict: true`). The setting only exists alongside
  required checks. Use GitHub's "Update branch" when a PR falls behind.
- **No no-op jobs.** The licence check ships per ecosystem: `job-license-check-go.yaml` only where
  there is a root `go.mod`, `job-license-check-npm.yaml` only where there is a root `package.json`.
- **Every job has a `timeout-minutes`** (10 for small checks, 15 to 30 for builds, scans and
  releases), so a hung job stops long before GitHub's 360-minute default. Jobs that call a reusable
  workflow (`uses:`) cannot take one; the called workflow's jobs carry it.
- **Prebuilt security tools.** Go Security installs the pinned gosec release binary and checks it
  against the published SHA-256, building the same version from source only when `go.mod` needs a
  newer Go than the binary was built with. govulncheck has no release binaries, so it is built once
  per version and Go toolchain and cached; the weekly run on `main` keeps that cache warm for PRs.
  To bump either tool, change the version (and for gosec the checksum) in `job-go-security.yaml`.
- **Required checks:** if the ruleset or branch protection lists required checks, use the job
  names: `✅ PR Checks` replaces `PR Title`, `PR Body`, `PR Hygiene` and `Gitleaks (secret scan)`.

## Engineering discipline

- Root-cause before fixing: confirm the actual cause with evidence before changing code; do not
  patch symptoms.
- Map every reference before removing a feature: trace its wiring across the tree first, preserve
  adjacent behavior that only looks related, and defer-and-flag an entangled piece rather than
  guessing it.
- Verify with evidence, not assertions: run the real check for what changed (lint, a full
  template/build render, `actionlint` for workflows) before calling it done. Green CI is necessary,
  not sufficient.
- One concern per branch/PR, even tiny ones — it keeps reviews and the drafted changelog clean.
- When PRs interact, state an explicit merge ORDER rather than opening them and walking away:
  anything a *tag* triggers needs its inputs on `main` first; a new *gate* (check) needs the
  violations it catches fixed first; a workflow that builds from committed content needs that
  content merged first.
- Semver framing: `breaking` only means breaking against a *released* version; removing something
  that was never shipped is not a breaking change.
- Cross-repo reconciliation goes through a neutral drop-zone outside both repos — never a repo
  inside a repo, and no accidental gitlinks/submodules.

## Logging

- Log generously in any code you write or touch: entry and exit of significant operations, the
  decisions and branches taken, retries, state changes, external calls (target, duration,
  outcome), and every error with its context. Finding a problem fast matters more than quiet code.
- Pick the level by detail: `trace` for step-by-step detail and values, `debug` for flow, `info`
  for lifecycle, `warn` and `error` for problems. The environment level filters the volume, so
  too much logging is fine.
- Levels by environment: local dev `trace`; dev cluster `debug`; pre-production (qa/staging)
  `info`; production `error`. Set them per environment through `LOG_LEVEL` and `LOG_FORMAT`, never
  by changing code. Library fallbacks stay safe when nothing is set.
- Format: local dev is human-readable (`LOG_FORMAT=console`), never JSON. Every cluster
  environment logs JSON. Local run targets (the Taskfile, or `.env.example` where there is no run
  target) set `LOG_LEVEL=trace LOG_FORMAT=console`.
- Deployment defaults are production-safe: a Helm chart's `values.yaml` defaults to `json` and
  `error`, and a dev values overlay sets `debug`.
- Never log secrets, tokens, or personal data, not even at `trace`. Log an opaque or keyed ID
  instead.

## Workflow

Issue (from a template; free-form issues are disabled) -> for sequential / multi-step work, a parent
issue with ordered **sub-issues** -> put it on the active **milestone** -> branch
`<type>/<issue#>-<slug>` -> code (comments cite the issue) -> PR with a Conventional Commit title
(the autolabeler sets the category label from the title), the template body, and a **closing
summary** before merge -> **squash** merge. The operator (@Bugs5382) is the assignee.

On merge, release-drafter drafts the next notes by label and `CHANGELOG.md` updates on `main` via the
changelog action -- **nothing tags automatically**. When the first push to main resolves the version,
rename the milestone to that version. The maintainer then **manually publishes the GitHub Release**,
which creates the tag with the finalized changelog (and triggers the publish where the repo ships a
package).

Keep public artifacts (issues, PRs, commit messages) free of references to local-only design notes.

## Releasing

This repo has no package manifest to bump and no Release Manager/version-bump workflow (removed at
scaffold: it assumed an `action.yml` floating major tag, which doesn't apply here). Release Drafter
still runs on every push to `main` and keeps a draft of the next release's notes by label; nothing
tags or publishes automatically. A release is: the maintainer reviews the draft notes, bumps
`Chart.yaml`'s `version`, and publishes the GitHub Release by hand, which cuts the `vX.Y.Z` tag. No
chart or manifest publishing happens on this box.

release-drafter cannot draft a first release (upstream release-drafter#1630): with no earlier
published release it proposes a version with "No changes" and a warning block. Do not fix that
draft by hand; the maintainer prepares the first release's notes.
