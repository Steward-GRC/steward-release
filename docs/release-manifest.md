# Release manifest and steward-migrate

## The manifest

[`release-manifest.json`](../release-manifest.json) is the one pinned file that names a release:

- every service image by digest (all fourteen images the chart installs, `ai-operator`, `web-staff`
  and `web-admin` included), with the image repository and no tag;
- the `core` ref: the `steward-core` commit the release was built against;
- the `proto` refs: for each service others call, the commit its callers pin their proto stubs to
  (the `STEWARD_<SERVICE>_REF` values of each repo's `proto-refs.env`);
- for each service, `store` (`postgres` when it owns a database, else `none`) and `migrateAfter`.

[`schemas/release-manifest.schema.json`](../schemas/release-manifest.schema.json) is the schema.
The loader also checks what a schema cannot: unique service names, proto refs and `migrateAfter`
only naming real services, and `migrateAfter` only between services that have a store.

The file in the tree is a template: every digest and ref is the all-zero placeholder and `release`
is `0.0.0-placeholder`. The maintainer fills it in when a release is cut. `--strict` rejects any
placeholder left, and `--apply` always runs strict, so a template can never be applied.

## Before you start

- Go 1.26 or newer, to build the tool (`go build ./cmd/steward-migrate`).
- Each service's migration files, one folder per service, for example
  `migrations/core/0001_baseline.up.sql`. The files live in each image at `/migrations`; copy them
  out of the images the manifest pins. The tool does not pull images.
- For every service that has a store, its database DSN in `STEWARD_MIGRATE_DSN_<SERVICE>` (upper
  case, `-` written as `_`, for example `STEWARD_MIGRATE_DSN_CORE`). DSNs come from the environment
  only: they are secrets, and a flag shows in process listings. Use a direct connection, as the
  services' own `MIGRATE_DSN` does.

## Running it

```bash
go run ./cmd/steward-migrate --manifest release-manifest.json --migrations-root migrations
```

That is a dry-run, the default: it reads each store's applied version and lists the versions that
would run, in order, and changes nothing. A dirty store (a migration that stopped half-way) or a
missing DSN is reported and blocks `--apply`.

```bash
go run ./cmd/steward-migrate --manifest release-manifest.json --migrations-root migrations --apply
```

`--apply` first plans every store, and runs nothing unless all of them are readable and clean. It
then migrates in order with the same call the services make at start-up, and stops at the first
failure. Running it again continues from there, because applied versions are recorded in each
database's own `schema_migrations` table.

## Order

Every service owns its own database and there are no cross-database dependencies, so order is not
needed for correctness today. The tool still runs in a fixed order, so a run is the same every
time: the foundation first (`audit`, which every service publishes to, then `identity`, then
`core`), then the services that call them. The order comes from `migrateAfter` in the manifest,
not from the proto refs: the call graph has cycles (`identity` and `workflow` call each other), a
migration order cannot. With no edge between two services, the one listed first in the manifest
goes first. A cycle or an unknown name is an error.

## Not covered

- Pulling images or extracting migration files from them.
- Rolling back. Down files exist in each service; a rollback needs its own plan (see
  [upgrade.md](upgrade.md)).
- The audit outbox tables that `identity` and `reporting` create at their own start-up with
  `go-outbox`, and the `vector` extension `ai` needs. Those stay with the services.
- Converting a database from the original app. `steward-workflow` `docs/data.md` and
  `steward-identity` `docs/data-model.md` name `steward-migrate` for that; it is a separate piece
  of work.
