# WAL-G (Lateos fork): PostgreSQL WAL Archiving & Backups

A maintained fork of [wal-g/wal-g](https://github.com/wal-g/wal-g), forked in
June 2026 at upstream commit
[`7e9f9055`](https://github.com/wal-g/wal-g/tree/7e9f90554506c260d08e521b350d0df306062a9e).
Upstream WAL-G remains actively maintained; this fork exists to maintain the
v0.14-era codebase with the additions listed below. Teams without a need for
those additions should use [upstream WAL-G](https://github.com/wal-g/wal-g).

## Attribution

This repository is a fork of the [original WAL-G project](https://github.com/wal-g/wal-g).

- Inherited code is licensed under the [Apache License 2.0](LICENSE) (upstream copyright: Citus Data Inc. — see [NOTICE](NOTICE))
- Code authored by Lateos is licensed under the [MIT License](LICENSE-MIT)
- See [COPYRIGHT.md](COPYRIGHT.md) for a path-by-path license map and [docs/FORK_PROVENANCE.md](docs/FORK_PROVENANCE.md) for the fork's provenance

## Status

- Fork point: upstream commit `7e9f9055` (2026-06-04). Upstream is active — latest release [v3.0.8](https://github.com/wal-g/wal-g/releases/tag/v3.0.8) (January 2026).
- Fork releases: [4 releases, June–July 2026](https://github.com/postgresql-tools/wal-g/releases).
- CI: unit tests with the race detector and all compression/encryption drivers ([unittests](.github/workflows/unittests.yml)), `go test -race ./...` ([test](.github/workflows/test.yml)), Windows-native build ([windows-native](.github/workflows/windows-native.yml)), Docker-based integration tests against PostgreSQL 10 and 18, MongoDB 7/8, Redis, MySQL, MariaDB, Greenplum, and etcd ([docker tests](.github/workflows/dockertests-par.yml)), golangci-lint, and license compliance ([license-check](.github/workflows/license-check.yml)). [View runs](https://github.com/postgresql-tools/wal-g/actions).
- The documentation site is not published yet (see [Roadmap](#roadmap)).

## Fork additions

- **`backup-verify`** — two-tier backup verification (Tier 1: sentinel integrity, manifest completeness, checksum coverage, decrypt canary; Tier 2: sampled tar-partition download) — [docs](docs/BACKUP-RECOVERY.md)
- **`doctor`** — preflight checks for config resolution, storage read/write/delete, crypter round-trip, PostgreSQL connectivity, WAL archiving, backup freshness, and free space vs. restore size — [docs](docs/BACKUP-RECOVERY.md#preflight-checks-with-doctor)
- **`pitr-window`** — reports the ranges of time the storage can actually be restored to, the gaps between them, and which backups can no longer serve a restore; `--min-window` makes it a CI gate against a retention policy that has stopped covering its RPO — [docs](docs/PostgreSQL.md#pitr-window)
- **`delete --explain`** — on every `delete` subcommand: what the delete would remove *and* the recovery window before and after it, with warnings for deletes that leave nothing restorable, open a gap, or strand backups in storage that can no longer be restored — [docs](docs/PostgreSQL.md#delete---explain)
- **Deployment metadata** — `--git-commit`, `--git-branch`, `--deploy-id` flags recorded in backup metadata (`cmd/pg/backup_push.go`)
- **Checksum inventory** — per-file SHA256 checksums stored at backup time and reported by `backup-verify`
- **Characterization tests** — golden-file regression detection ([`internal/characterization`](internal/characterization), [`pkg/storages/postgres/characterization_test.go`](pkg/storages/postgres/characterization_test.go))
- **Dependency hardening** — audited dependency baseline and fix trail — [docs/security-audit.md](docs/security-audit.md)
- **License compliance CI** — automatic enforcement of the Apache-2.0/MIT structure — [.github/workflows/license-check.yml](.github/workflows/license-check.yml)

## Inherited capabilities (from upstream WAL-G)

- Point-in-time recovery via continuous WAL archiving and incremental backups
- Storage backends: S3, Google Cloud Storage, Azure, Alibaba OSS, Swift, SSH, and local filesystem — [docs/STORAGES.md](docs/STORAGES.md)
- Encryption: AWS KMS, Yandex Cloud KMS, OpenPGP, and libsodium — [overview docs](docs/README.md)
- Monitoring: Prometheus exporter ([cmd/pg/exporter](cmd/pg/exporter/README.md), extended in this fork with `backup-verify` metrics) and statsd/graphite telemetry
- `wal-verify` — WAL integrity and timeline verification

## Quick Start

### Installation

Binaries are published with each [release](https://github.com/postgresql-tools/wal-g/releases).

Docker images, a Homebrew formula, and a Helm chart are not yet available (see [Roadmap](#roadmap)).

### Configure & Backup

```bash
# Set the storage prefix (example: S3)
export WALG_S3_PREFIX=s3://your-bucket/wal-g
export AWS_REGION=us-east-1

# Create a backup
wal-g backup-push

# List backups
wal-g backup-list

# Restore the latest backup
wal-g backup-fetch /tmp/restore LATEST
```

## Documentation

- [Backup & Recovery](docs/BACKUP-RECOVERY.md)
- [PostgreSQL](docs/PostgreSQL.md)
- [Storage backends](docs/STORAGES.md)
- [Overview (upstream documentation)](docs/README.md)
- [Monitoring (Prometheus exporter)](cmd/pg/exporter/README.md)
- [Security audit trail](docs/security-audit.md)
- [Fork provenance](docs/FORK_PROVENANCE.md)

## Community

- [Report issues](https://github.com/postgresql-tools/wal-g/issues)
- [Security policy](SECURITY.md)

## Roadmap

Planned, not yet built (no dates committed):

- Backblaze B2 storage backend
- Helm chart
- Homebrew formula and Docker images
- Binary-level compatibility test suite against upstream v0.14.x artifacts
- Automated restore-test tooling (RTO/RPO validation)
- Published documentation site (mkdocs/readthedocs)
- Public metrics dashboard (CI and release statistics)

## Contributing

This repository is vendor-maintained: we are currently not accepting external
code contributions, pull requests, bug fixes, or feature submissions. Pull
requests opened by external contributors may be closed unmerged. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the development setup used by
maintainers.

## License

Inherited code is licensed under the [Apache License 2.0](LICENSE) (upstream
copyright: Citus Data Inc. — see [NOTICE](NOTICE)). Code authored by Lateos is
licensed under the [MIT License](LICENSE-MIT). See [COPYRIGHT.md](COPYRIGHT.md)
for a path-by-path license map.

---

**Maintained by [Lateos](https://lateos.ai)**
