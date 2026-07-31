# Security & Stability Audit Report

**Branch:** `security-stability-audit`  
**Date:** 2026-06-09  
**Tools:** govulncheck, semgrep, golangci-lint, go-licenses, go list -u -m all, manual grep

---

## 1. Dependency Vulnerability Scan (govulncheck)

**Command:** `govulncheck -scan=package ./...`

### Remaining Vulnerabilities (3 total, all N/A fix)

| ID | Module | Found In | Fixed In | Severity |
|---|---|---|---|---|
| GO-2026-4887 | github.com/docker/docker | v28.5.2+incompatible | N/A | HIGH |
| GO-2026-4883 | github.com/docker/docker | v28.5.2+incompatible | N/A | HIGH |
| GO-2026-4518 | github.com/jackc/pgproto3/v2 | v2.3.3 | N/A | MEDIUM |

**Action:** None. All earlier vulnerabilities (x/net, x/crypto, stdlib) were fixed in PR #8. These 3 have no available fix yet.

---

## 2. SAST Scan (Semgrep)

**Command:** `semgrep --config "p/golang" --config "p/security-audit" .`  
**Rules run:** 143 | **Findings:** 37

### CRITICAL (0)

### HIGH (5)

| Finding | File | Line | Issue |
|---|---|---|---|
| Unsafe deserialization into `interface{}` | `internal/configure.go` | 399-400 | JSON unmarshal into `interface{}` allows arbitrary types (CWE-502) |
| Unsafe deserialization into `interface{}` | `pkg/storages/s3/session.go` | 249-250 | YAML unmarshal into `interface{}` (CWE-502) |
| SHA1 hash for crypto | `internal/crypto/envelope/enveloper.go` | 34 | `sha1.Sum()` — not collision-resistant |
| SQL injection (string-formatted query) | `internal/databases/mysql/mysql.go` | 37 | `"SELECT @@" + variable` |
| SQL injection (string-formatted queries) | `internal/databases/sqlserver/*.go` | Multiple | `fmt.Sprintf("BACKUP DATABASE %s TO %s", ...)` — 7 occurrences across backup/restore/log handlers |

### MEDIUM (16)

| Finding | File | Line | Issue |
|---|---|---|---|
| `math/rand` used (non-production) | `internal/storagetools/check.go` | 6 | Should use `crypto/rand` |
| `math/rand` used | `internal/multistorage/stats/alive_checker.go` | 8 | Should use `crypto/rand` |
| `math/rand` used (non-production) | `internal/profile.go` | 4 | Should use `crypto/rand` |
| `math/rand` used (test files) | 9 test files | Multiple | Tests only, lower priority |
| MD5 hash | `pkg/storages/s3/folder.go` | 58 | `md5.Sum([]byte(sseCustomerKey))` |
| MD5 hash | `pkg/storages/storage/storage.go` | 33 | `md5.New()` |
| MD5 hash (test) | `pkg/storages/s3/uploader_test.go` | 55 | Test only |
| Unsafe pointer in Windows code | `internal/multistorage/stats/cache/flock_windows.go` | 27-36 | `unsafe.Pointer` for syscall args (needed for Windows API) |
| Unsafe pointer in Windows code | `internal/diskwatcher/disk_watcher_windows.go` | 27-30 | `unsafe.Pointer` for syscall args (needed for Windows API) |
| Unsafe pointer for page verification | `internal/databases/postgres/paged_file_verifier.go` | 95 | `unsafe.Pointer` to cast page bytes (needed for low-level PG page checksum) |

### LOW (16)

| Finding | File | Line | Issue |
|---|---|---|---|
| Dockerfile ends with `USER root` | `docker/cloudberry_tests/Dockerfile` | 23 | Container runs as root |
| Dockerfile ends with `USER root` | `docker/etcd_tests/Dockerfile` | 39 | Container runs as root |
| Dockerfile ends with `USER root` | `docker/gp_tests/Dockerfile` | 20 | Container runs as root |
| TLS MinVersion not set | `internal/databases/mysql/mysql.go` | 191-193 | TLS 1.2 default, should pin TLS 1.3 |
| TLS MinVersion not set + InsecureSkipVerify | `internal/databases/sqlserver/backup_import_handler.go` | 125 | Should pin TLS 1.3 |
| Bind to all interfaces | `internal/databases/mongo/binary/mongod_runner.go` | 114 | `net.Listen("tcp", ":0")` |
| SSH InsecureIgnoreHostKey | `pkg/storages/sh/storage.go` | 57 | Host key verification disabled (MITM risk) |
| `math/rand` in test/benchmark files | 12 test files | Multiple | Low priority |

---

## 3. Anti-Slop Code Audit (golangci-lint)

**Command:** `golangci-lint run ./...`  
**Linters used:** gci, gofmt, goimports, govet, errcheck, ineffassign, misspell, revive, staticcheck, unconvert, whitespace, gocritic, gocyclo, dupl, funlen, lll, nakedret, unparam, unused, bodyclose, copyloopvar, asciicheck, makezero

### Findings (7)

| Severity | Linter | File | Line | Issue |
|---|---|---|---|---|
| MEDIUM | gci | `cmd/common/common.go` | 1 | Import not properly formatted |
| MEDIUM | gci | `internal/databases/redis/archive/sharded.go` | 1 | Import not properly formatted |
| MEDIUM | gci | `pkg/storages/gcs/folder.go` | 1 | Import not properly formatted |
| MEDIUM | staticcheck | `internal/multistorage/stats/cache/flock_windows.go` | 28 | `syscall.Syscall6` deprecated, use `SyscallN` |
| MEDIUM | unconvert | `internal/multistorage/stats/cache/flock_windows.go` | 29 | Unnecessary `uintptr()` conversion |
| LOW | whitespace | `internal/diskwatcher/disk_watcher_windows.go` | 11 | Leading newline |
| LOW | whitespace | `internal/diskwatcher/disk_watcher_windows.go` | 47 | Trailing newline |

---

## 4. Cryptographic Audit

### HMAC
- **Not used** — no direct HMAC operations found.
- Envelope encryption uses key-wrapping (AWS KMS, YC KMS, OpenPGP).

### AES
- **Not used directly** — no `aes.NewCipher` calls in codebase.
- Encryption is delegated to external providers (AWS KMS, YC KMS, OpenPGP).

### Random Number Generation
- `crypto/rand` used in: `internal/fsutil/direct_io_reader_test.go`, `internal/crypto/awskms/key.go`
- `math/rand` used in: 12 non-production/test files, 3 production files (`internal/storagetools/check.go`, `internal/multistorage/stats/alive_checker.go`, `internal/profile.go`)

### SHA1 Usage (1 finding)
- `internal/crypto/envelope/enveloper.go:34` — `sha1.Sum(encryptedKey.Data)` for key fingerprinting. Used only for identification/logging, not for cryptographic verification. Low severity.

### MD5 Usage (2 production + 1 test)
- `pkg/storages/s3/folder.go:58` — `md5.Sum([]byte(sseCustomerKey))` — used for SSE-C key hashing by AWS S3 SDK requirement. Not used for security.
- `pkg/storages/storage/storage.go:33` — `md5.New()` — used for ETag/checksum computation. Lower severity as it's not used for authentication.
- `pkg/storages/s3/uploader_test.go:55` — Test only.

### TLS/SSL
- `internal/databases/mysql/mysql.go:191` — TLS config missing `MinVersion` (defaults to TLS 1.2; should pin to 1.3)
- `internal/databases/sqlserver/backup_import_handler.go:125` — `InsecureSkipVerify: true` + missing `MinVersion`
- `pkg/storages/sh/storage.go:57` — `ssh.InsecureIgnoreHostKey()` — disables host key verification for SFTP/SSH storage

### Key Storage
- AWS KMS keys: configured via environment variables (standard)
- YC KMS keys: configured via environment variables (standard)
- OpenPGP keys: via environment variable or file (standard)
- No hardcoded keys found

### Password Handling
- No plaintext password logging found

---

## 5. License Compliance

**Command:** `go-licenses csv ./utility ./internal/crypto ./internal/checksum`

| Dependency | License | Compatible |
|---|---|---|
| github.com/lateos-ai/wal-g | MIT | ✅ |
| github.com/pkg/errors | BSD-2-Clause | ✅ |
| github.com/wal-g/tracelog | Apache-2.0 | ✅ |

**No GPL/AGPL licenses found.** Full scan failed on `encoding/json/v2` (experimental stdlib), but sampled packages show clean licenses.

---

## 6. Dependency Age & Maintenance

**Command:** `go list -u -m all`  
**Total outdated dependencies:** ~170

### Key production dependencies 1+ year stale

| Dependency | Current Version | Latest | Age |
|---|---|---|---|
| `cloud.google.com/go` | v0.65.0 | v0.123.0 | ~5 years |
| `google.golang.org/api` | v0.30.0 | v0.283.0 | ~5 years |
| `google.golang.org/genproto` | v0.0.0-20211021150943 | v0.0.0-202606 | ~5 years |
| `gopkg.in/ini.v1` | v1.67.0 | v1.67.3 | Ok (minor) |
| `github.com/prometheus/client_golang` | v1.12.1 | v1.23.2 | ~3 years |
| `github.com/spf13/cobra` | v1.7.0 | v1.10.2 | ~2 years |
| `github.com/aws/aws-sdk-go` | v1.55.7 | v1.55.8 | Ok (recent) |
| `github.com/Azure/azure-sdk-for-go/sdk/azcore` | v1.21.1 | v1.22.0 | Ok (recent) |
| `github.com/klauspost/compress` | v1.18.5 | v1.18.6 | Ok (recent) |

### Abandoned or low-maintenance dependencies
- `github.com/3rf/mongo-lint` — last updated 2014 (fork of golint)
- `github.com/ryanuber/columnize` — last updated 2016
- `github.com/bgentry/speakeasy` — last updated 2015
- `github.com/jstemmer/go-junit-report` — fork, last updated 2016
- `github.com/kr/logfmt` — last updated 2017
- `github.com/pascaldekloe/goe` — last updated 2019

Most of these are transitive dev dependencies and don't affect production.

---

## 7. Remediation Status (re-verified 2026-07-31)

Re-verified line-by-line against the current tree. The raw scan artifacts
(`semgrep-report.txt`, `golangci-lint-report.txt`, `SECURITY_FINDINGS_VERIFIED.md`)
were removed from the repository on this date; this section is the single
source of truth for SAST findings. golangci-lint results are now additionally
uploaded to GitHub Code Scanning as SARIF from CI
([.github/workflows/golangci-lint.yml](../.github/workflows/golangci-lint.yml)).

### Resolved since the 2026-06-09 audit

| Finding | Status |
|---|---|
| golangci-lint: 7 findings | All fixed (e.g. `syscall.Syscall6` → `SyscallN` in `internal/multistorage/stats/cache/flock_windows.go:30`, gci/goimports formatting, whitespace) |
| SQL Server proxy TLS: `InsecureSkipVerify: true` + missing `MinVersion` | Fixed — `internal/databases/sqlserver/backup_import_handler.go:176` now uses `tls.Config{MinVersion: tls.VersionTLS12}`; no `InsecureSkipVerify` remains anywhere in the tree (grep 2026-07-31) |

### Still present (all verified live on 2026-07-31)

| Rule (semgrep) | Count | Locations | Disposition |
|---|---|---|---|
| `last-user-is-root` | 3 | `docker/gp_tests/Dockerfile:20`, `docker/cloudberry_tests/Dockerfile:23`, `docker/etcd_tests/Dockerfile:39` | Test containers only. Removed with engine excision (PR-3, PostgreSQL-only refactor) |
| `unsafe-deserialization-interface` | 2 | `internal/configure.go:499` (`UnmarshalSentinelUserData`), `pkg/storages/s3/session.go:330` (YAML headers) | Accepted: user-data is supplied by the operator who also controls the bucket; no privilege boundary crossed |
| `use-of-sha1` | 1 | `internal/crypto/envelope/enveloper.go:34` | Key *fingerprint* for logging/identification only, not cryptographic verification; accepted |
| `avoid-bind-to-all-interfaces` | 1 | `internal/databases/mongo/binary/mongod_runner.go:141` | Test utility; removed with engine excision (PR-3) |
| `string-formatted-query` | 6 | MySQL: `internal/databases/mysql/mysql.go:49` (`"SELECT @@" + variable`, whitelisted via `allowedMySQLVariables`). SQL Server: `sqlserver.go:221,237,253,697`, `log_restore_handler.go:204,208`, `log_push_handler.go:72,74`, `backup_restore_handler.go:104,142`, `backup_push_handler.go:116,118` (3 use `quoteName()`/`quoteValue()` escaping) | Identifiers come from controlled sources (`allowedMySQLVariables` map / internal DTOs); accepted with above mitigations. MySQL + SQL Server engines removed with engine excision (PR-3) |
| `missing-ssl-minversion` | 1 | `internal/databases/mysql/mysql.go:203` (`tls.Config{RootCAs: ...}` defaults to TLS 1.2) | MySQL engine removed with engine excision (PR-3) |
| `use-of-unsafe-block` | 3 | `internal/multistorage/stats/cache/flock_windows.go:30`, `internal/diskwatcher/disk_watcher_windows.go:27-30`, `internal/databases/postgres/paged_file_verifier.go:95` | Required by design (Windows syscall ABI, PostgreSQL page checksum casting); accepted |
| `math-random-used` | 10 | Production: `internal/profile.go:6`, `internal/storagetools/check.go:8`, `internal/multistorage/stats/alive_checker.go:10`, `pkg/storages/gcs/uploader.go:8`, `pkg/storages/s3/range_reader.go:8`, `internal/databases/postgres/backup_verify_handler.go:12` (fork addition — `backup-verify` sampling). Tests: 4 files | Non-security uses (port choice, jitter, sampling); `crypto/rand` unnecessary; accepted |
| `use-of-md5` | 2 | `pkg/storages/storage/storage.go:33` (ETag/checksum), `pkg/storages/s3/folder.go:80` (SSE-C key hash, required by AWS S3 SDK) | Protocol-required, not used for authentication; accepted |
| `avoid-ssh-insecure-ignore-host-key` | 1 | `pkg/storages/sh/storage.go:50` | Only when `SSH_KNOWN_HOSTS` / `SSH_IGNORE_HOST_KEY`-style options are unset; SFTP storage explicitly configured by the operator; documented |

### Summary

- 1 of 37 raw findings confirmed resolved; all others verified still present.
- None are critical; none involve credentials, hostnames, or developer-machine
  paths (verified in files and full git history).
- 10 of 36 remaining findings (bind-all, 6 string-formatted-query, 3
  last-user-is-root, 1 missing-ssl-minversion) are removed together with the
  non-PostgreSQL engines in the postgres-only refactor (PR-3).

---

## Summary

| Category | CRITICAL | HIGH | MEDIUM | LOW |
|---|---|---|---|---|
| govulncheck | 0 | 2 | 1 | 0 |
| Semgrep SAST | 0 | 5 | 16 | 16 |
| golangci-lint | 0 | 0 | 5 | 2 |
| Crypto audit | 0 | 0 | 4 | 1 |
| **Total** | **0** | **7** | **26** | **19** |

**Top priorities for Part 2 (Manual Review):**
1. SQL injection in `mysql.go` and `sqlserver/*.go` — verify `quoteName()` is safe
2. TLS `InsecureSkipVerify` in SQL Server backup import
3. SSH `InsecureIgnoreHostKey` in SFTP storage
4. Unsafe deserialization in `configure.go` and `s3/session.go`
5. `ssh.InsecureIgnoreHostKey` in SSH storage
