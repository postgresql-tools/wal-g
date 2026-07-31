# Contributing to WAL-G

This repository is vendor-maintained by the Lateos team and is currently not
accepting external code contributions, pull requests, bug fixes, or feature
submissions. This file documents the internal development setup for
maintainers.

## Development Setup

```bash
git clone https://github.com/postgresql-tools/wal-g.git
cd wal-g
go mod download
go test ./...
```

Note: builds and tests require `GOEXPERIMENT=jsonv2`.

## Reporting Issues

Use GitHub Issues. Include:
- Steps to reproduce
- Expected vs actual behavior
- Your environment (OS, Go version, PostgreSQL version)
