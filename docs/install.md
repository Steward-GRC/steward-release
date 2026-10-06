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
3. For a managed Postgres install (the default): nothing to create ahead of time. The
   `Bugs5382/helm-postgres-ha` dependency provisions its own credentials Secrets.
4. For a bring-your-own Postgres install (see below): create each service's password Secret first,
   in the install namespace, before `helm install`.
5. The image repository and tag for each service, once its own repo publishes a release. The
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

The chart never ships a plaintext default for either path: a managed instance's password comes
from the Secret `Bugs5382/helm-postgres-ha` generates, and a bring-your-own instance's password
comes from the Secret you name in `passwordSecretName`. Leaving either unset fails the template
render rather than falling back to anything.

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

## Health probes

Each service alias picks its probe type in `values.yaml` (`<service>.probe.type`):

- `http` (the default): `GET /readyz` and `/livez` on `<service>.probePort` (8081). A service that
  serves them on its main HTTP port sets `probePort` to that port (gateway does), and the chart
  renders the port once.
- `grpc`: the kubelet's native `grpc.health.v1` probe on the service's main port, for a service with
  no HTTP listener (audit). Your NetworkPolicy implementation must let the node reach pod ports,
  which every common one does by default.

## The authz policy bundle

`core`, `gateway` and `ai` (the services that evaluate access in-process) pull the steward-authz
Rego policy bundle from an init container image (`<service>.opaBundle.image`) into a shared,
read-only volume at start-up. Point it at your own bundle image, or steward-authz's published one
once it ships.
