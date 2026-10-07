# Install

This chart installs a Steward platform on any Kubernetes cluster. It's the first-class install
path alongside the appliance (a separate repo for a single-node, closed-shell OS image); use this
chart when you already run Kubernetes and want to bring your own.

## Before you install

1. A Kubernetes cluster, 1.29 or newer, with its own OIDC service-account issuer reachable at
   `https://kubernetes.default.svc.cluster.local` (the default on most clusters since 1.21;
   override `<service>.workloadAuth.oidc.issuer`/`jwksUrl` in `charts/_service/values.yaml` for a
   cluster with a different `--service-account-issuer`).
2. A namespace for the release (the examples below use `steward`).
3. The PdfRender and PolicyAIJob CRDs, installed once per cluster by a cluster-admin (see "Custom
   resources and API access"), unless you set `crds.install: true`.
4. For a managed Postgres install (the default): cert-manager running in the cluster (each
   instance's TLS; see "Cluster-scoped objects" for the issuer and the Secret-only path), and
   nothing else to create ahead of time. The `Bugs5382/helm-postgres-ha` dependency provisions
   its own credentials Secrets.
5. For a bring-your-own Postgres install (see below): create each service's password Secret first,
   in the install namespace, before `helm install`.
6. The image repository and tag for each service, once its own repo publishes a release. The
   defaults in `values.yaml` are placeholders (`ghcr.io/steward-grc/steward-<service>:v0.1.0`);
   override them with `--set <service>.image.tag=...` or your own values file.

## A managed-Postgres install

```bash
helm dependency build .
helm install steward . -f values.yaml --namespace steward --create-namespace
```

Every service gets its own `Bugs5382/helm-postgres-ha` instance (one Postgres cluster per service,
never shared), at the replica count set in `values.yaml` (1 by default). `helm dependency build`
downloads `postgres-ha` from `oci://ghcr.io/bugs5382/charts`; until the maintainer publishes that
chart's first release, this step fails (a tracked, reported gap — see the repo's port plan). Once
it's published, re-run `helm dependency build` and install as above.

## A bring-your-own-Postgres install

Disable the managed instance for each service you want to point at your own Postgres, and set that
service's `postgres.external.*` values. [`values-byo-postgres-example.yaml`](../values-byo-postgres-example.yaml)
shows every service switched over; mix and match per service is also fine — a cluster can run some
services on the managed chart and others against an existing database.

```bash
# Create the password Secret for each external database first, for example:
kubectl create secret generic steward-identity-postgres-external \
  --namespace steward --from-literal=password='...'

helm install steward . -f values.yaml -f values-byo-postgres-example.yaml \
  --namespace steward --create-namespace
```

**ai needs pgvector.** ai's first migration creates `vector` columns, so its database needs the
[pgvector](https://github.com/pgvector/pgvector) extension. On your own Postgres, install pgvector
on the server (a stock `postgres` image doesn't have it; the `pgvector/pgvector` image does), then,
as a superuser, in ai's database and before ai's first start:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

Without it ai's first migration fails and the pod never becomes Ready. `helm install` prints this
requirement in its notes for every service with `postgres.requiredExtensions` set. The managed
`ai-postgres` instance runs the `pgvector/pgvector` image (same Postgres major as the official one,
pinned by digest in `values.yaml`) and creates the extension itself.

The chart never ships a plaintext default for either path: a managed instance's password comes
from the Secret `Bugs5382/helm-postgres-ha` generates, and a bring-your-own instance's password
comes from the Secret you name in `passwordSecretName`. Leaving either unset fails the template
render rather than falling back to anything.

Each service gets its database as `DATABASE_DSN`
(`postgres://<user>@<host>:<port>/<database>?sslmode=<sslmode>`, built from its `postgres`
values) and the password as `PGPASSWORD`, read from that Secret; the password never appears in
the connection URL.

## High availability

[`values-ha-example.yaml`](../values-ha-example.yaml) raises every service to multiple replicas and
every managed Postgres instance to a 3-member quorum:

```bash
helm install steward . -f values.yaml -f values-ha-example.yaml --namespace steward --create-namespace
```

Replica counts are always user-set (`values.yaml`'s own defaults, or this file's); there's no
separate "HA mode" switch, only higher numbers.

## Secrets this chart expects by name

Every `*SecretName` value in `values.yaml` names an existing Kubernetes Secret or SealedSecret; the
chart only ever reads from them, never writes a plaintext credential of its own. See
[Service-to-service authentication](service-to-service-auth.md) for the workload-identity pieces,
which need no Secret at all (they ride the cluster's own service-account tokens and OIDC issuer).

## Custom resources and API access

The chart ships two CustomResourceDefinitions: `PdfRender` (`renders.steward-grc.com`, with
pdf-renderer) and `PolicyAIJob` (`ai.steward-grc.com`, with ai). They are vendored from each
service's generated manifest at a pinned commit listed in `scripts/crds-upstream.txt` (minus the
controller-gen version annotation); `scripts/sync-crds.sh` refreshes them and CI fails if a
vendored copy drifts from its pin.

CRDs are cluster-scoped, so the chart doesn't install them by default (see "Cluster-scoped
objects" below). Install them once per cluster, as cluster-admin, before the first
`helm install`:

```bash
kubectl apply --server-side -f charts/_pdf-renderer-crds/crds/ -f charts/_ai-crds/crds/
```

On a cluster this release owns alone, `--set crds.install=true` lets `helm install` install them
from `crds/` instead (first install only; Helm never upgrades or deletes CRDs).

Every pod runs with `automountServiceAccountToken: false` except the three services that call the
Kubernetes API, each with a namespaced Role written from its own needs and nothing cluster-wide:

| Service | Why | Role |
|---|---|---|
| pdf-renderer (service account `steward-pdf-renderer-operator`) | reconciles PdfRenders into render Jobs | pdfrenders (+ status, finalizers), batch jobs, events, leader-election leases |
| delivery | creates PdfRenders for PDF export and watches their status | pdfrenders: create, list, watch |
| ai | creates and reads PolicyAIJobs | policyaijobs: get, create |

The render Jobs run as `steward-pdf-renderer`, a service account with no RBAC and no API token; it
is the caller name delivery's allow-list expects on the HTML fetch. Set the Jobs' image with
`pdf-renderer.baseEnv` `RENDERER_IMAGE` (or an `env` entry of the same name), and create the
object-storage Secret the Jobs read (`steward-pdf-renderer-s3` by default) before the first export.

## Cluster-scoped objects

With default values the chart creates no cluster-scoped object: no CRD, ClusterRole,
ClusterRoleBinding, admission webhook, ClusterIssuer, IngressClass, StorageClass or
PriorityClass. RBAC is namespaced Roles and RoleBindings only. Anything cluster-scoped is shared
by every release on the cluster, so it comes from the cluster and each one is an explicit opt-in.
CI renders the defaults and both examples with `--include-crds` and fails on any cluster-scoped
kind (`scripts/check-cluster-scoped.sh`).

| Object | Default | Use the existing one | Opt in |
|---|---|---|---|
| PdfRender and PolicyAIJob CRDs | not installed | pre-install with `kubectl apply` (above) | `crds.install: true` |
| cert-manager Issuer for managed Postgres TLS | a namespaced self-signed CA Issuer per instance | `<service>-postgres.tls.certManager.issuerRef: {kind: Issuer or ClusterIssuer, name}` | none: the chart never creates a ClusterIssuer |

cert-manager itself, an ingress controller and any IngressClass come from the cluster; the chart
installs none of them. Each managed Postgres instance signs its certificates through cert-manager
by default. To use the cluster's existing issuer for all of them, layer
[`values-shared-cluster-example.yaml`](../values-shared-cluster-example.yaml) (a ClusterIssuer
named `internal-ca`; change the name, or use `kind: Issuer` for one in the release namespace). To
skip cert-manager, set `<service>-postgres.tls.certManager.enabled: false` and
`<service>-postgres.tls.existingSecret` to a Secret with `tls.crt`, `tls.key` and `ca.crt` (see
the helm-postgres-ha README for the DNS names it needs).

## Health probes

Each service alias picks its probe type in `values.yaml` (`<service>.probe.type`):

- `http` (the default): `GET /readyz` and `/livez` on `<service>.probePort` (8081). A service that
  serves them on its main HTTP port sets `probePort` to that port (gateway does), and the chart
  renders the port once.
- `grpc`: the kubelet's native `grpc.health.v1` probe on the service's main port, for a service with
  no HTTP listener (audit). Your NetworkPolicy implementation must let the node reach pod ports,
  which every common one does by default.

A service with a second listener declares it in `<service>.extraPorts` (delivery's internal HTTP
port, 8082, which the PDF renderer's Jobs fetch policy HTML from). The chart passes the number to
the service in the named variable and fails the render if any two ports of one service collide.

The main port's number reaches the service in the variable `<service>.port.env` names:
`GRPC_PORT` for the Go services, `METRICS_PORT` for pdf-renderer, `PORT` for web, and none for the
gateway, whose default listen address already matches its 8080.

## The authz policy bundle

`core`, `gateway` and `ai` (the services that evaluate access in-process) pull the steward-authz
Rego policy bundle from an init container image (`<service>.opaBundle.image`) into a shared,
read-only volume at start-up. Point it at your own bundle image, or steward-authz's published one
once it ships.
