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

Every pod runs with `automountServiceAccountToken: false` except the four that call the Kubernetes
API. Each has a namespaced Role holding only the calls its code makes, and nothing cluster-wide:

| Workload (service account) | Why | Role |
|---|---|---|
| pdf-renderer operator (`steward-pdf-renderer-operator`) | reconciles PdfRenders into render Jobs | pdfrenders: get, list, watch; pdfrenders/status: update; pdfrenders/finalizers: update (the Jobs' blocking owner reference); batch jobs: get, list, watch, create; events: create, patch; leases: create, then get and update on its own lease only |
| ai operator (`steward-ai-operator`) | runs PolicyAIJobs, deletes finished ones, schedules the nightly relationship job | policyaijobs: get, list, watch, create, delete; policyaijobs/status: update; events: create, patch; leases: create, then get and update on its own lease only |
| delivery (`steward-delivery`) | creates PdfRenders for PDF export and watches their status | pdfrenders: create, list, watch |
| ai (`steward-ai`) | creates and reads PolicyAIJobs | policyaijobs: get, create |

pdf-renderer's own RBAC markers grant more (every verb on pdfrenders, update, patch and delete on
Jobs); the chart grants only what its controller calls. Each operator watches its own namespace
only.

The render Jobs run as `steward-pdf-renderer`, a service account with no RBAC and no API token; it
is the caller name delivery's allow-list expects on the HTML fetch. Set the Jobs' image with
`pdf-renderer.baseEnv` `RENDERER_IMAGE` (or an `env` entry of the same name), and create the
object-storage Secret the Jobs read (`steward-pdf-renderer-s3` by default) before the first export.

### ai's operator

ai ships two binaries: the gRPC server (`ai`) and the operator (`ai-operator`, the image built from
steward-ai's `Dockerfile.operator`) that runs the PolicyAIJobs the server creates. Without the
operator ai's jobs are created and never run. `ai-operator.enabled` is on by default; turn it off
together with ai (`ai.enabled`). The operator uses ai's database and reads the same settings as ai:
set the same `postgres` (or `postgres.external`) values, and add the same `RABBITMQ_URL`,
`REDIS_ADDR`, `AI_SETTINGS_KEY` and embeddings and generation settings to `ai-operator.env` as to
`ai.env`. It serves its probes on 8081 and controller metrics on 9090, has no Service and no
workload identity (it calls no Steward service), and uses leader election, so a second replica only
takes over.

### PDF export

delivery's PDF export creates a PdfRender per request; pdf-renderer's operator turns it into a
render Job, which fetches the policy HTML from delivery's internal port (8082) with its own
`steward`-audience token (delivery admits only `<namespace>/steward-pdf-renderer` there) and writes
the PDF to object storage, where delivery signs the download link. The chart sets
`PDF_EXPORT_ENABLED=true` for delivery while pdf-renderer is on. To run without pdf-renderer, set
`pdf-renderer.enabled: false` and `PDF_EXPORT_ENABLED=false` in `delivery.env`; the render fails
if only the first is set, since nothing would reconcile delivery's PdfRenders.

Both sides read the same object store from one Secret you create (the chart holds no credential
and never creates it). The render Jobs load it whole; delivery reads its keys into its `S3_*`
settings and puts the bucket in each PdfRender, so the Jobs write where delivery signs links:

```bash
kubectl -n steward create secret generic steward-pdf-renderer-s3 \
  --from-literal=AWS_S3_ENDPOINT=https://s3.example.org \
  --from-literal=S3_BUCKET=steward-pdf \
  --from-literal=AWS_REGION=us-east-1 \
  --from-literal=AWS_S3_FORCE_PATH_STYLE=true \
  --from-literal=AWS_ACCESS_KEY_ID=... --from-literal=AWS_SECRET_ACCESS_KEY=...
```

| Secret key | delivery setting | Render Job setting |
|---|---|---|
| `AWS_S3_ENDPOINT` | `S3_ENDPOINT` | `AWS_S3_ENDPOINT` |
| `S3_BUCKET` | `S3_BUCKET` (sent to the Job as its output bucket) | - |
| `AWS_REGION` | `S3_REGION` | `AWS_REGION` |
| `AWS_S3_FORCE_PATH_STYLE` | `S3_FORCE_PATH_STYLE` | `AWS_S3_FORCE_PATH_STYLE` |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | `S3_ACCESS_KEY`, `S3_SECRET_KEY` | the same |

To use an existing Secret under another name, set `pdf-renderer` `S3_SECRET_NAME` to it (an
`env` entry) and point delivery's six `S3_*` entries at it in `delivery.env`; the render fails
if delivery and the Jobs name different Secrets. Every key is optional to delivery: until the
Secret exists delivery runs with PDF export off and reports `pdfexport` degraded, and it reads
the Secret only at start, so restart it (`kubectl -n steward rollout restart
deploy/steward-delivery`) after creating or changing the Secret.

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

- `http` (the default): `GET /readyz` and `/livez` on `<service>.probePort` (8081; 8080 for audit).
  A service that serves them on its main HTTP port sets `probePort` to that port (gateway does), and
  the chart renders the port once.
- `grpc`: the kubelet's native `grpc.health.v1` probe on the service's main port, for a service with
  no HTTP listener (none of the defaults today). Your NetworkPolicy implementation must let the
  node reach pod ports, which every common one does by default.

A service with a second listener declares it in `<service>.extraPorts` (delivery's internal HTTP
port, 8082, which the PDF renderer's Jobs fetch policy HTML from). The chart passes the number to
the service in the named variable and fails the render if any two ports of one service collide.

The main port's number reaches the service in the variable `<service>.port.env` names: `GRPC_PORT`
for the Go services, `METRICS_PORT` for the pdf-renderer and ai operators, `PORT` for the web apps,
and none for the gateway, whose default listen address already matches its 8080.

## Web apps

steward-web ships two apps, each its own image built from steward-web's Dockerfile (`APP=staff`
or `APP=admin`): `web-staff` (the staff app, image `steward-web-staff`) and `web-admin` (the
admin app, image `steward-web-admin`). Each has its own Deployment, Service (port 3000) and
replica count, and turns off with its own `enabled`. Both get `GATEWAY_URL` from the gateway's
Service, server-render against it and proxy the browser's `/query` and `/collab/ws` to it. Both
serve `/livez` and `/readyz` on port 3000; `/readyz` fails while the gateway is unreachable. Both
sign in through the gateway's own session endpoint, not Kratos directly, so neither needs a
Kratos URL of its own. The pods run as the image's own user (UID/GID 1001).

Neither app is exposed by default. To expose one, set its `ingress` (namespaced; the cluster's
ingress controller and IngressClass, or the cluster default when `className` is empty; a TLS
Secret you create, by name):

```yaml
web-staff:
  ingress:
    enabled: true
    className: nginx
    hosts:
    - host: steward.example.org
      paths:
      - {path: /, pathType: Prefix}
    tls:
    - {hosts: [steward.example.org], secretName: steward-web-staff-tls}
web-admin:
  ingress:
    enabled: true
    className: nginx
    hosts:
    - host: steward-admin.example.org
      paths:
      - {path: /, pathType: Prefix}
    tls:
    - {hosts: [steward-admin.example.org], secretName: steward-web-admin-tls}
```

Each app's origin must also be in the gateway's allowed origins (its `ALLOWED_ORIGINS` setting).

### Second-factor enforcement at the public edge

gateway's `MFA_ENFORCE` (a value in `values.yaml`, default `edge`, matching gateway's own
default) asks a signed-in user for a second factor only when the sign-in carries the header the
public edge is supposed to set (`X-Steward-Edge: public`), which the web apps forward to gateway
when it's present on the incoming request. Each app's `ingress.edgeHeader` (default `true` when
that app's Ingress is enabled) sets that header through an ingress-nginx
`configuration-snippet` annotation; a cluster running a different ingress controller needs its
own equivalent annotation in that app's `ingress.annotations` instead, with `edgeHeader: false` so
the two don't collide. Set `gateway.env`'s `MFA_ENFORCE` entry to `always` or `never` to change the
enforcement itself.

gateway's `COOKIE_INSECURE` (also a `gateway.env` value, default `false`) drops the session
cookie's Secure flag. Local and kind installs only, for a gateway reached over plain HTTP; never
set it to `true` in a value this chart ships for anything else.

### The dev quick login

steward-web's sign-in page can show a dev-only quick login, built only into a dev image (the
Dockerfile's `DEV_QUICK_LOGIN=true` build argument) and switched on at runtime by
`<app>.devQuickLogin.enabled`, which mounts an existing ConfigMap or Secret's accounts JSON and
sets `STEWARD_DEV_QUICK_LOGIN`/`STEWARD_DEV_QUICK_LOGIN_USERS` for you:

```yaml
web-staff:
  image:
    tag: dev-quick-login
  devQuickLogin:
    enabled: true
    configMapName: steward-web-staff-dev-quick-login
```

Local and kind installs only; the chart's own shipped defaults never turn this on, and the
runtime switch does nothing against a release image that wasn't built with `DEV_QUICK_LOGIN=true`.

## Service addresses

The chart gives every service the addresses of the services it calls (`CORE_GRPC_ADDR`,
`IDENTITY_GRPC_ADDR`, identity's `WORKFLOW_GRPC_ADDR` and `OBLIGATIONS_GRPC_ADDR` for account
merges and delete checks, the gateway's `STEWARD_<SERVICE>_ADDR`, the web apps' `GATEWAY_URL`), so a
default install needs none in `env`. Each alias lists what it calls in `<service>.calls`; the chart
builds `steward-<callee>:<port>` from the callee's Service name and its port in
`global.servicePorts`.
Each `global.servicePorts` entry must equal that alias's `port.number`, and the render fails when
they differ, so a port change sets both. An `env` entry with the same name replaces a derived
address, for a callee that runs outside the release.

## Access decisions

`core` and `gateway` decide access in-process with the steward-authz Go module, built into each
image, and `ai` applies the same read rules in its own queries. The chart mounts no policy bundle
and needs no bundle image.

## Who can work officer cases

reporting's `REPORTING_OFFICER_GROUPS` (a value in `values.yaml`, empty by default) names who can
open and work reporting cases: a comma-separated list of local platform group ids or identity
provider group names. Find a platform group's id on the admin app's platform groups page (backed
by identity's `ListGroups`), or use an identity provider group name already mapped through an
existing group-claim mapping. Leave it empty and reporting logs a warning at startup: nobody can
open a case until it's set.
