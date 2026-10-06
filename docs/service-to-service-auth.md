# Service-to-service authentication

Every gRPC call between Steward services carries the caller's own identity, verified by the
callee. Nothing here is mTLS-only trust or a shared secret.

## How it works

1. **Caller:** each pod that calls another service gets a projected, audience-scoped
   Kubernetes service-account token at `/var/run/secrets/steward/token` (audience `steward`,
   refreshed automatically by the kubelet). The caller sends it as `authorization: Bearer <token>`
   gRPC metadata on every call, re-reading the file each time rather than caching it.
2. **Callee:** verifies the token against the cluster's OIDC JWKS, maps the verified service
   account to a caller name, and checks that name against its own allow-list
   (`WORKLOAD_ALLOWED_SERVICEACCOUNTS`) plus a per-method policy in its own code. A missing,
   invalid or unlisted caller is refused and audited.
3. **Fail closed:** the only way to turn this off is `WORKLOAD_AUTH=disabled`, meant for a local
   run only. A service running with it disabled logs a warning at start-up and reports degraded on
   its readiness check.

## What this chart wires for every service alias

| Env var | Set by | Meaning |
|---|---|---|
| `WORKLOAD_TOKEN_FILE` | caller | where the projected token lives |
| `WORKLOAD_AUDIENCE` | callee | the audience the token must carry (`steward`) |
| `WORKLOAD_OIDC_ISSUER`, `WORKLOAD_OIDC_JWKS_URL` | callee | where to verify the token; default to the cluster's own API server |
| `WORKLOAD_OIDC_CA_FILE` | callee | the CA for that endpoint; defaults to the CA every pod already mounts |
| `WORKLOAD_OIDC_BEARER_FILE` | callee | a bearer token the callee itself presents while verifying, when it needs one; defaults to its own caller token path |
| `WORKLOAD_ALLOWED_SERVICEACCOUNTS` | callee | the caller list below, as `<namespace>/steward-<caller>` |
| `WORKLOAD_AUTH` | callee | `enabled` (default) or `disabled` |

A `NetworkPolicy` per callee narrows which pods can even reach its port to the same caller list —
defence in depth, never the only control; the actual enforcement is the token check above.

## The caller list, by callee

| Callee | Callers |
|---|---|
| `identity` | gateway, core, workflow, obligations, reporting, collab |
| `core` | gateway, workflow, delivery, collab, obligations |
| `workflow` | gateway |
| `obligations` | gateway, reporting |
| `audit` | gateway, reporting |
| `delivery` | gateway, pdf-renderer (pdf-renderer calls delivery's internal render port over the same mechanism) |
| `collab` | gateway |
| `ai` | gateway |
| `reporting` | gateway |

`gateway` has no caller list of its own here: its inbound traffic is the web app over the
browser-origin edge (session and CSRF protected, a different mechanism), not another service's
workload token. `gateway` is still a **caller** to every service above.

`core` ships with `WORKLOAD_AUTH: "disabled"` in this chart's defaults (see `values.yaml`): it
flips to `enabled` once every one of its callers (workflow, obligations, delivery, collab, gateway)
is confirmed sending a token, since turning it on before that would lock out a live caller.

## Changing the caller list

Add or remove a name from a service's `workloadAuth.allowedServiceAccounts` in `values.yaml`, then
`helm upgrade`. The chart expands each bare name to `<namespace>/steward-<name>`, so a caller's own
alias name here must match its own `aliasName` value elsewhere in the chart.
