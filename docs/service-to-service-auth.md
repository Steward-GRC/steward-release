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

## The JWKS fetch credentials

The API server serves `/openid/v1/jwks` to any service account (the
`system:service-account-issuer-discovery` binding), but only for a token whose audience it
accepts; it rejects the caller token's `steward` audience with a 401. So for every enabled callee
the chart projects, into the same read-only volume at `/var/run/secrets/steward`:

| File | Source | Used for |
|---|---|---|
| `token` | service-account token, audience `steward` (callers only) | calls to other Steward services |
| `jwks/token` | service-account token, no audience (the API server's default) | the discovery and JWKS fetch |
| `kube-ca/ca.crt` | the namespace's `kube-root-ca.crt` ConfigMap | verifying the API server's TLS |

`automountServiceAccountToken` stays off, so no pod gets the general-purpose token. For an issuer
outside the cluster, set `<service>.workloadAuth.oidc.issuer`/`jwksUrl` and point
`oidc.caFile`/`oidc.bearerFile` at your own files.

## Which services verify callers today

Workload auth is implemented in **core, delivery, reporting, collab and ai** (callees) and in
**gateway** (a caller only). Only those aliases are wired with `workloadAuth.callee`/`caller`.

**Gap:** identity, workflow, obligations and audit have no workload-auth support yet. They don't
read any `WORKLOAD_*` setting, so the chart sets none for them, and they accept any caller that
can reach their port. Their only optional caller check is mTLS with a `*_TRUSTED_CALLERS` list,
which this chart doesn't wire. Until they gain workload auth, the only control in front of them is
their NetworkPolicy (rendered from the same caller list below), which is defence in depth, not
authentication. workflow and obligations are not wired as callers either, for the same reason.

## What this chart wires for every service alias

| Env var | Set by | Meaning |
|---|---|---|
| `WORKLOAD_TOKEN_FILE` | caller | where the projected token lives |
| `WORKLOAD_AUDIENCE` | callee | the audience the token must carry (`steward`) |
| `WORKLOAD_OIDC_ISSUER`, `WORKLOAD_OIDC_JWKS_URL` | callee | where to verify the token; default to the cluster's own API server |
| `WORKLOAD_OIDC_CA_FILE` | callee | the CA for that endpoint: `/var/run/secrets/steward/kube-ca/ca.crt`, the namespace's `kube-root-ca.crt` ConfigMap |
| `WORKLOAD_OIDC_BEARER_FILE` | callee | the token the callee presents on the discovery and JWKS fetch: `/var/run/secrets/steward/jwks/token`, a second projected token with the API server's default audience |
| `WORKLOAD_ALLOWED_SERVICEACCOUNTS` | callee | the caller list below, as `<namespace>/steward-<caller>` |
| `WORKLOAD_AUTH` | callee | left unset (enabled, `authMode: enabled`), or `disabled` alone with none of the rows above (`authMode: disabled`) |

The chart emits exactly one of the two shapes per callee: the verification block (every callee row
above except `WORKLOAD_AUTH`), or `WORKLOAD_AUTH=disabled` on its own. The services reject both
together, and any other `WORKLOAD_AUTH` value, at start-up; the chart fails the render for any
`authMode` other than `enabled` or `disabled`, and for an enabled callee with an empty caller list.

A `NetworkPolicy` per alias with a caller list narrows which pods can even reach its port to that
list — defence in depth; for the services that verify tokens the actual enforcement is the token
check above (see the gap above for the ones that don't).

## The caller list, by callee

| Callee | Callers | Enforced by |
|---|---|---|
| `identity` | gateway, core, workflow, obligations, reporting, collab | NetworkPolicy only (gap) |
| `core` | gateway, workflow, delivery, collab, obligations | token check (disabled by default, below) + NetworkPolicy |
| `workflow` | gateway | NetworkPolicy only (gap) |
| `obligations` | gateway, reporting | NetworkPolicy only (gap) |
| `audit` | gateway, reporting | NetworkPolicy only (gap) |
| `delivery` | gateway, pdf-renderer (the renderer's Jobs fetch from delivery's internal HTTP port with a token the operator projects into each Job) | token check + NetworkPolicy |
| `collab` | gateway | token check + NetworkPolicy |
| `ai` | gateway | token check + NetworkPolicy |
| `reporting` | gateway | token check + NetworkPolicy |

`gateway` has no caller list of its own here: its inbound traffic is the web app over the
browser-origin edge (session and CSRF protected, a different mechanism), not another service's
workload token. `gateway` is still a **caller** to every service above.

`core` ships with `workloadAuth.authMode: disabled` in this chart's defaults (see `values.yaml`), so
it runs with `WORKLOAD_AUTH=disabled`: it flips to `enabled` once every one of its callers
(workflow, obligations, delivery, collab, gateway) is confirmed sending a token, since turning it on
before that would lock out a live caller. workflow and obligations can't send one until they gain
workload auth.

## Changing the caller list

Add or remove a name from a service's `workloadAuth.allowedServiceAccounts` in `values.yaml`, then
`helm upgrade`. The chart expands each bare name to `<namespace>/steward-<name>`, so a caller's own
alias name here must match its own `aliasName` value elsewhere in the chart.
