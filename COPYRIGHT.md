# COPYRIGHT and license map

This file explains which parts of this repository are under which license.
It is a plain-language map; the license texts in `LICENSE` (Apache-2.0) and
`LICENSE-MIT` (MIT) are the controlling terms. See `NOTICE` for attribution
and provenance.

## Inherited code — Apache License 2.0

The great majority of this repository (~98.5% of tracked lines) is derived
from the upstream WAL-G project, `https://github.com/wal-g/wal-g`, fork
point commit `7e9f90554506c260d08e521b350d0df306062a9e` (2026-06-04),
merged on 2026-06-06. Upstream's license record: "Copyright 2017 Citus
Data Inc.", Apache License 2.0.

This includes, with few exceptions, everything under:

- `cmd/`
- `internal/`
- `pkg/`
- `utility/`
- `main/` (except the files listed below)
- `testtools/`, `tests_func/`, `test/`, `testdata/` (inherited parts)
- `docker/`, `docs/` (inherited parts), `Makefile`, `.github/workflows/` (inherited parts), `go.mod`, `go.sum`

Per Apache-2.0 §4(b), every inherited file modified after the fork point
carries a one-line notice at the top of the file:

    Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

The one exception is `go.sum`, whose format does not permit comment lines.

## Fork-authored code — MIT License

Code originally written for this fork by Lateos, licensed under the MIT
License (see `LICENSE-MIT`):

- `main.go`, `main_test.go`
- `cmd/pg/backup_verify.go`
- `internal/characterization/golden.go`
- `internal/checksum/checksum_test.go`
- `internal/crypto/libsodium/cgo_stubs.c`
- `internal/databases/postgres/backup_verify_handler.go`,
  `internal/databases/postgres/backup_verify_handler_test.go`
- `internal/databases/postgres/deploy_metadata.go`,
  `internal/databases/postgres/deploy_metadata_test.go`
- `internal/databases/postgres/tar_ball_file_packer_test.go`,
  `internal/tar_ball_file_packer_test.go`
- `pkg/storages/postgres/characterization_test.go`
- Fork-authored documentation: `NOTICE`, `COPYRIGHT.md`, `README.md`,
  `CONTRIBUTING.md`, `SECURITY.md`, `BLA.md`, `docs/BACKUP-RECOVERY.md`,
  `docs/ENCRYPTION_AUDIT.md`, `docs/FORK_PROVENANCE.md`,
  `docs/security-audit.md`, and fork-added workflows under
  `.github/workflows/` (`compatibility.yml`, `docs-publish.yml`,
  `security-scan.yml`, `test.yml`).

## How the licenses combine

- You may use the inherited code under Apache-2.0 and the fork-authored
  code under MIT, or the combined work under either license subject to
  the conditions of both (Apache-2.0 §4 for the inherited parts).
- The Apache-2.0 conditions that survive distribution of the combined
  work are met as follows:
  - §4(a) give recipients a copy of the license — this file, `LICENSE`,
    `LICENSE-MIT`, and `NOTICE` are shipped in every release artifact.
  - §4(b) modified files carry prominent change notices — the one-line
    notice above, plus this file.
  - §4(c) copyright/attribution notices retained — upstream's notice
    ("Copyright 2017 Citus Data Inc.") is preserved in `NOTICE`; no
    source-header notices were removed by this fork.
  - §4(d) NOTICE file — the upstream project had no NOTICE file at the
    fork point, so there is no upstream NOTICE content to reproduce;
    this fork's NOTICE is provided for attribution.

## Third-party notices

- Go module dependencies are governed by their own licenses; the
  release binary statically links brotli (MIT) and, when built with
  `USE_LZO=1`, the LZO library (GPL-2.0-or-later, system library).
  See `go.mod` and the dependency license inventory in
  `docs/FORK_PROVENANCE.md`.
- Files under `internal/contextio/` and `internal/fsutil/` carry their
  own third-party copyright headers (Olivier Mengué; beego Authors).
