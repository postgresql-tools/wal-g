// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

# Compatibility Matrix

## Supported PostgreSQL Versions

This fork is tested against the following PostgreSQL versions, all of which are currently
supported by the [PostgreSQL Global Development Group (PGDG)](https://www.postgresql.org/support/versioning/):

| Version | Status          | Notes                                  |
|---------|-----------------|----------------------------------------|
| PG 15   | Supported       | General support until Nov 2026         |
| PG 16   | Supported       | General support until Nov 2027         |
| PG 17   | Supported       | General support until Nov 2028         |
| PG 18   | Supported       | Current stable release                 |

**Note:** PostgreSQL 10 was previously included in CI but has been removed because it reached
end-of-life in November 2024. Testing against an EOL version does not validate compatibility
for production deployments on supported PostgreSQL releases.

**Note:** PostgreSQL 19 is not yet included in CI. It has not been released by the PostgreSQL
Global Development Group (expected September 2026), so no `postgresql-19` packages are
available from PGDG yet. CI coverage for PG 19 will be added once it is released.

## Test Coverage

Each supported PostgreSQL version runs the following integration tests via GitHub Actions:

- Full backup push and fetch
- Remote backup (S3)
- Delete retain full
- Catchup operations
- WAL performance test
- Crypto/encryption
- Delta backup full scan
- WAL-E compatibility
- Configuration validation
- Ghost table handling
- Partial restore
- WAL prefetch
- WAL receive
- Multiple delta backups chaining
- Streamed full backup
- Copy composer variants (copy, rating, database)
- Delete before permanent full
- Delete target delta
- Delete garbage collection
- Backup verify (two-tier verification)

Tests run against MinIO as the S3-compatible object storage backend.

## CI Pipeline

The compatibility tests execute in `.github/workflows/dockertests-par.yml` under the
`parallel` job, which delegates to `dockertests.yml`. Each test runs inside a Docker
container built from `docker/pg/Dockerfile` with the appropriate `PG_MAJOR` argument.

### Build Targets

Make targets for building PostgreSQL container images:

```bash
make pg15_build_image    # Build PG 15 base + test images
make pg16_build_image    # Build PG 16 base + test images
make pg17_build_image    # Build PG 17 base + test images
make pg18_build_image    # Build PG 18 base + test images
```

### Running Tests Locally

```bash
# Run a specific test on a specific version
make TEST="pg15_full_backup_test" pg_integration_test
make TEST="pg16_remote_backup_test" pg_integration_test
make TEST="pg17_delete_retain_full_test" pg_integration_test
make TEST="pg18_full_backup_test" pg_integration_test

# Run all tests for a version
make TEST="pg15_tests" pg_integration_test
make TEST="pg16_tests" pg_integration_test
make TEST="pg17_tests" pg_integration_test
make TEST="pg18_tests" pg_integration_test
```

## Object Storage

All compatibility tests use **MinIO** as the S3-compatible object storage backend.
MinIO provides a local, self-hosted alternative to AWS S3 that is functionally equivalent
for the operations WAL-G uses (put, get, list, delete, multipart upload).

Known divergences from real S3:
- No cross-region replication (not used by WAL-G)
- Different consistency model for concurrent reads during writes (WAL-G uses sequential
  operations, so this is not a practical concern)
- Event notifications and lifecycle rules (not used by WAL-G)

For production deployments, replace MinIO configuration with real S3, GCS, Azure Blob,
or any other supported storage backend without changes to WAL-G behavior.

## Backward Compatibility

### With Upstream WAL-G

This fork shares a common ancestor with upstream WAL-G at commit `7e9f9055` (tagged as
upstream v0.14.1). Backward compatibility between this fork's backups and upstream WAL-G
is verified through round-trip testing:

1. Produce a backup with one version
2. Restore it with another version
3. Verify correctness with `pg_checksums` and `amcheck`

See the [CI pipeline](https://github.com/postgresql-tools/wal-g/actions) for live results.

### Between Versions

Backups produced by this fork on PG 10 can be restored on PG 15-18 and vice versa, subject
to PostgreSQL's standard backward-compatibility guarantees. Major version upgrades may require
additional steps (e.g., `pg_upgrade`) beyond what WAL-G handles.

## Deprecated / Removed

| Version | Status     | Reason                           |
|---------|------------|----------------------------------|
| PG 10   | EOL (Nov 2024) | Removed from CI; no longer supported by PGDG |
| PG 11+12+13+14 | EOL      | Not tested; not supported by PGDG |
