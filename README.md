# steward-release 📦

> 🧭 Helm charts for a cloud-native Steward install, alongside the appliance

This repo holds the Helm charts for a self-hosted Steward install on any Kubernetes cluster: one
umbrella chart composing every service on a reusable per-service chart, each with its own
high-availability Postgres instance (or your own, brought in instead), service-to-service
authentication wired in from the start, and health-checked probes.

- **One reusable chart:** every service alias (`identity`, `core`, `workflow`, `obligations`,
  `audit`, `delivery`, `pdf-renderer`, `collab`, `ai`, `ai-operator`, `reporting`, `gateway`,
  `web-staff`, `web-admin`) composes [`charts/_service`](charts/_service); a new service is a values block, never a new chart.
- **Postgres:** each data-owning service gets its own [Bugs5382/helm-postgres-ha](https://github.com/Bugs5382/helm-postgres-ha)
  instance, replica count set by you (1 by default, a 3-member quorum in
  [`values-ha-example.yaml`](values-ha-example.yaml)); bring your own instead with
  [`values-byo-postgres-example.yaml`](values-byo-postgres-example.yaml).
- **Shared clusters:** default values install no cluster-scoped object (no CRDs, ClusterRoles,
  webhooks or ClusterIssuers); CRDs are pre-installed or opted in with `crds.install`, and managed
  Postgres TLS can use the cluster's existing cert-manager issuer
  ([`values-shared-cluster-example.yaml`](values-shared-cluster-example.yaml)). See
  [docs/install.md](docs/install.md).
- **Service-to-service auth:** calls carry a projected, audience-scoped service-account token,
  verified against the cluster's OIDC JWKS with a per-service caller allow-list, and a
  `NetworkPolicy` in depth. Not every service verifies tokens yet; see
  [docs/service-to-service-auth.md](docs/service-to-service-auth.md) for which do.
- **Health:** every service's readiness fails while a required dependency is down; liveness checks
  only the process.

## 🚀 Install

```bash
helm dependency build .
helm install steward . -f values.yaml --namespace steward --create-namespace
```

See [docs/install.md](docs/install.md) for the Secrets to create first, the bring-your-own-Postgres
and high-availability paths, and [docs/upgrade.md](docs/upgrade.md) for upgrades.

## 📚 Docs

- [Install](docs/install.md): Secrets, bring-your-own Postgres, high availability.
- [Upgrade](docs/upgrade.md).
- [Service-to-service authentication](docs/service-to-service-auth.md).

## 🛠 Develop

```bash
helm lint . && helm lint charts/_service
helm template steward . -f values.yaml --namespace steward
```

Pipe the template output to `kubeconform` (pinned in `.github/workflows/checks.yml`) to validate
the rendered manifests without a cluster. `task test` runs the helm-unittest suites.

The install test ([`ci/kind/`](ci/kind)) builds every service image from the commit pinned in
`ci/kind/images.txt`, installs the chart on a kind cluster with minimal bring-your-own
dependencies, and fails unless every Deployment becomes Ready and the gateway's `/readyz` answers
200. CI runs it on every pull request; locally:

```bash
for a in $(awk '!/^#/ && NF {print $1}' ci/kind/images.txt); do ci/kind/build-image.sh "$a"; done
KIND_CLUSTER=steward-ci ci/kind/run.sh   # creates the cluster if needed; never deletes it
```

A re-run against the same cluster reuses the images already on its node and the Secrets it
generated the first time, so its Postgres keeps accepting the service passwords.

## 🙏 Acknowledgements

Steward was originally written by [@Bugs5382](https://github.com/Bugs5382).

## ⚖️ License

Apache-2.0 (c) 2026 The Steward Authors
