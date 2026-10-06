# Upgrade

```bash
helm dependency build .
helm upgrade steward . -f values.yaml [-f values-ha-example.yaml | -f values-byo-postgres-example.yaml] \
  --namespace steward
```

- **Read the diff first:** `helm diff upgrade steward . -f values.yaml --namespace steward` (the
  `helm-diff` plugin) before an upgrade that changes more than an image tag.
- **Image tags move one service at a time** in normal operation: bump `<service>.image.tag` and
  upgrade, watch that service's rollout and `/readyz`, then move to the next. Upgrading every
  service's tag in one release is only for a version bump that's the same across the whole
  platform.
- **Postgres:** `Bugs5382/helm-postgres-ha` manages its own rolling upgrade (streaming replication,
  lease-based failover); this chart never restarts a Postgres instance by touching anything other
  than the values the chart passes it (`replicas`, the image tag the subchart pins, resource
  requests). Changing a data-owning service's database name, user or `passwordSecretName` is a
  migration, not an upgrade — coordinate it with `steward-migrate` once that tool exists.
- **Migrations:** each service's own baseline migration runs itself, inside its own image, on
  start-up. This chart has no migration Job of its own.
- **Workload auth:** flipping a service's `workloadAuth.authMode` from `disabled` to `enabled` (the
  planned path for `core`, once every caller presents a token) is itself an upgrade: do it only
  after confirming every service in that service's `allowedServiceAccounts` list already sends a
  token (check each caller's own `workloadAuth.caller: true` is live first).
- **CRDs:** Helm installs the chart's CustomResourceDefinitions on first install only and never
  upgrades or deletes them. When an upgrade bumps a pin in `scripts/crds-upstream.txt`, apply the
  new CRDs first: `kubectl apply --server-side -f charts/_pdf-renderer-crds/crds/ -f
  charts/_ai-crds/crds/`.
- **Rollback:** `helm rollback steward <revision> --namespace steward`. A managed Postgres instance
  is not rolled back by this (its own chart's upgrade history is separate); a schema change that
  isn't backward-compatible with the previous image needs its own rollback plan.
