# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

WAL-G is an actively maintained fork of the original WAL-G project, providing PostgreSQL WAL archiving, point-in-time recovery, and disaster recovery capabilities. The codebase is a multi-database backup tool supporting PostgreSQL, MySQL, MongoDB, Redis, FoundationDB, Greenplum, SQL Server, and Etcd.

- **Module:** `github.com/lateos-ai/wal-g`
- **Go Version:** 1.25.7+
- **License:** MIT (original Apache 2.0)

## Building & Installation

WAL-G is organized as a multi-database project. Each database has its own main entry point under `cmd/` with associated build targets.

### Primary Build Targets

```bash
# PostgreSQL (most common)
make pg_build          # Build wal-g for PostgreSQL
make pg_install        # Build and install to $GOBIN
make pg_test           # Build + run full test suite

# MySQL
make mysql_build
make mysql_install
make mysql_test

# MongoDB
make mongo_build
make mongo_install

# Redis
make redis_build
make redis_install
make redis_test

# FoundationDB, Greenplum, SQL Server, Etcd - similar pattern
make fdb_build         # and fdb_install, fdb_integration_test
make gp_build          # and gp_install
make sqlserver_build
make etcd_build
```

### Common Build Flags

These flags enable optional compression/encryption formats:

```bash
make pg_build USE_BROTLI=1         # Enable Brotli compression
make pg_build USE_LZO=1            # Enable LZO compression (adds system dependency)
make pg_build USE_LIBSODIUM=1      # Enable libsodium encryption
```

### Building with External Dependencies

Some features require pre-built system libraries. The Makefile orchestrates this:

```bash
make deps              # Download vendored Go deps + build external dependencies
make link_external_deps  # Link brotli/libsodium into vendor/
make go_deps           # Update and vendor Go dependencies
```

## Testing

### Unit Tests

```bash
make unittest          # Run all unit tests (excludes libsodium to avoid CGO issues)
make coverage          # Generate test coverage report (opens HTML)
```

Unit tests run against the vendored dependencies and respect `BUILD_TAGS` for optional features.

### Integration Tests

Integration tests use Docker Compose and test against real database instances:

```bash
make pg_integration_test  # PostgreSQL integration tests (pg10 + pg18)
make mysql_integration_test
make redis_integration_test
make mongo_test
make fdb_integration_test
make gp_integration_test
make etcd_integration_test
```

Set `TEST` to run specific test suites:

```bash
make pg_integration_test TEST="pg18_tests"  # PostgreSQL 18 only
make pg_integration_test TEST="pg10_ssh_backup_test"  # SSH backup specific test
```

### Running a Single Unit Test

```bash
# Find the test file (e.g., cmd/pg/some_test.go)
# Then run it with go test directly:
go test -mod vendor -v ./internal/somepackage -run TestSpecificName
```

## Code Architecture

### Directory Structure

- **`cmd/`** — Entry points for each supported database (pg, mysql, mongo, redis, fdb, gp, sqlserver, etcd). Each has a `main/` subdirectory with `main.go`.
- **`cmd/common/`** — Shared CLI utilities (flags, completion, signal handling).
- **`internal/`** — Core backup/restore logic:
  - **`backup*.go`** — Backup creation and metadata handling
  - **`compression/`** — Compression drivers (gzip, lz4, lzma, zstd, brotli, lzo)
  - **`crypto/`** — Encryption (AES-256-GCM, libsodium, OpenPGP, AWS KMS, Yandex Cloud KMS)
  - **`storage/`** — Cloud backend abstraction (S3, GCS, Azure Blob, B2, local filesystem)
  - **`config/`** — Configuration parsing and validation
  - **`walparser/`** — PostgreSQL WAL parsing
  - **`daemon/`** — Daemon mode for background processes
  - **`multistorage/`** — Multi-cloud storage routing
  - **`statistics/`** — Metrics and telemetry
- **`tests_func/`** — Behavioral (BDD) tests using Cucumber/godog

### Key Architectural Patterns

1. **Database Abstraction** — Common upload/download/backup logic in `internal/` is used by all database commands. Database-specific logic lives in `cmd/{db}/` (e.g., WAL parsing is PostgreSQL-specific in `internal/walparser/`).

2. **Storage Layer** — `storage.Folder` interface abstracts cloud backends. All backup I/O goes through this layer, making multi-cloud support transparent to backup logic.

3. **Compression & Encryption** — Pluggable via `compression.Algorithm` and `crypto.Crypter` interfaces. Build tags select which implementations are compiled in.

4. **Configuration** — Uses Viper (config file + env vars + flags). Config validation happens in `internal/config/`.

5. **Vendor-Heavy Build** — All external dependencies are vendored (`go mod vendor`). External C libraries (libsodium, brotli, lzo) are built separately and linked during `make deps`.

### Testing Patterns

- **Unit tests** live alongside code (`*_test.go`)
- **Integration tests** in `tests_func/` use godog (BDD syntax). They spin up real database containers and test end-to-end flows
- **Docker Compose** (`docker-compose.yml`) defines test services (databases, S3, etc.)

## Linting & Code Quality

```bash
make lint              # Run golangci-lint locally
make docker_lint       # Run linting in Docker (isolated environment)
make fmt               # Format with gofmt + goimports
```

**Linter Config:** `.golangci.yml`
- Go 1.25 with modules in readonly mode
- Focuses on correctness (dupl, errcheck, govet, staticcheck) over style
- High thresholds for complexity (gocyclo: 25, funlen: 250 lines)
- Test files excluded from most checks

## Release Workflow

The project uses semantic versioning with git tags. The Makefile reads tags to inject version info:

```bash
WALG_VERSION := git tag -l --points-at HEAD  # Release version from tag
GIT_REVISION := git rev-parse --short HEAD   # Short commit hash
BUILD_DATE   := $(shell date ...)            # Build timestamp
```

These are baked into the binary via `-ldflags`.

## Dependencies of Note

- **Cobra** — CLI framework
- **Viper** — Configuration management
- **AWS SDK**, **Azure SDK**, **GCP Cloud Storage** — Cloud backends
- **pglogrepl** — PostgreSQL logical replication
- **mongo-driver** — MongoDB backup
- **Testify** — Testing utilities

## Common Development Tasks

### Add a New Compression Algorithm

1. Create `internal/compression/{algo}/{algo}.go` implementing `compression.Algorithm`
2. Add import to `internal/compression/computils/factory.go`
3. Add build tag in `Makefile` if needed (e.g., `USE_ZSTD`)
4. Unit test in `*_test.go`
5. Integration test in `tests_func/` to verify backup/restore roundtrip

### Add a New Cloud Backend

1. Implement `storage.Folder` interface in a new package
2. Register in `internal/storage/adapter.go`
3. Add integration test

### Fix a PostgreSQL-Specific Bug

Likely in:
- `cmd/pg/` for CLI logic
- `internal/walparser/` for WAL parsing
- General `internal/` code for backup/restore logic

### Run PostgreSQL Tests Only (During Development)

```bash
# Quick iteration without full Docker build:
make pg_build           # Build the binary
# Then run tests manually:
docker compose up pg10_tests  # Or specify a specific test service
```

## Gotchas & Notes

1. **LibSodium/LZO require system dependencies** — If `USE_LIBSODIUM=1` or `USE_LZO=1` are set, the build will fail unless the respective libraries are installed. See `.github/workflows/release.yml` for how CI handles this.

2. **MacOS-specific flags** — Recent changes standardize CGO flags. On macOS, LZO uses `-llzo2` (no `-Bstatic/-Bdynamic`); see recent commits for details.

3. **Vendored build** — Always use `-mod vendor` in go commands to avoid network fetches. The Makefile does this consistently.

4. **Test data cleanup** — Untracked directories like `internal/data*` and `internal/databases/postgres/*` are test artifacts. Safe to delete if disk space is needed.

5. **100% Backward Compatibility** — This fork is a drop-in replacement for WAL-G v0.14.1. Do not break the CLI API or S3 object format without major version bump.

## Recent Focus Areas

Recent commits indicate active work on:
- **macOS compatibility** — Fixing CGO flags for platform-specific C libraries
- **Backup verification** — New `backup-verify` command (Tier 1 + Tier 2 checks)
- **Deployment metadata** — `--git-commit`, `--git-branch`, `--deploy-id` flags for tracing backups
- **Characterization tests** — Regression detection via golden-file comparison
