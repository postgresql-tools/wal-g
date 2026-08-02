# Backup Recovery

## Verifying backups without a full restore

WAL-G provides `backup-verify` to check backup integrity without performing a full
restore. This works in two tiers:

### Tier 1: Metadata verification (default)

Runs automatically when `wal-g backup-verify [<backup-name>]` is invoked. No data blocks
are downloaded — only metadata objects (sentinel, files metadata, WAL folder listing).
Completes in seconds with near-zero egress cost.

Checks performed:

- **Sentinel integrity** — the backup sentinel JSON is fetched and parsed. A corrupt
  or missing sentinel is reported immediately.
- **Manifest completeness** — each tar partition referenced in `files_metadata.json`
  is confirmed to exist in the tar partition folder. Missing parts are reported by
  name.
- **Checksum coverage** — reports how many files in the backup have a stored SHA256
  checksum. Backups taken before the checksum feature (pre-v0.14.2) will show
  `0/N files have stored checksums`.
- **Deploy metadata** — if the sentinel contains deployment metadata (git commit,
  branch, deploy ID), it is displayed. Otherwise `none` is reported.
- **WAL chain continuity** — the IntegrityCheckRunner scans WAL segments from the
  backup's finish LSN forward to the latest available segment and reports any gaps.
  This check is **informational only** and does not affect the exit code — WAL
  segments may have been legitimately archived off. Use `--target-lsn` or
  `--target-time` to scope the check to a range you know is retained.
- **Decrypt canary** — the smallest tar partition is fetched and its first tar header
  is read, proving that the crypter and compression codec configured *on this host*
  can actually open the backup's data. Every other Tier 1 check reads metadata only,
  so without this a green Tier 1 says nothing about whether the key available here
  can decrypt the bytes — the failure that otherwise surfaces during a restore, when
  it is too late to act on. Entry bodies are never read, so the transfer is bounded
  to the head of one object regardless of partition size.

  The canary is **skipped** when `--sample` is set, because Tier 2 decrypts a whole
  sample of partitions and makes it redundant. Pass `--no-canary` to stay strictly
  metadata-only and fetch no object data at all. A failed canary fails the run
  (exit code 1).

### Tier 2: Spot-check verification (`--sample <pct>`)

Downloads a random sample of tar partitions and verifies their content. Requires
storage egress proportional to the sample percentage.

- Sampling is **seeded** (`--seed`) for reproducible test runs.
- For each sampled part:
  - **If the backup has stored SHA256 checksums**: the part is downloaded, decrypted,
    decompressed, and each file's SHA256 is recomputed and compared against the stored
    value. A mismatch is a strong indicator of bitrot or storage corruption.
  - **If the backup predates checksum support**: the part is downloaded and confirmed
    to be readable (decrypts, decompresses, and has a valid tar structure), but this is
    labelled as a weaker check — byte-level integrity cannot be confirmed without a
    stored checksum.

### Understanding the output

A passing verification reports:

```
Backup: base_000000010000000000000001
Tier: 1

--- Tier 1: Metadata Verification ---
Sentinel: OK
Files metadata: OK (142 file(s))
Checksum coverage: 141/142 files have stored checksums
Missing parts: none
Deploy metadata: commit=abc123... branch=feature/foo deploy_id=deploy-42
Decrypt canary: OK (part part_003.tar.br, 4194304 bytes, crypter=libsodium)
WAL chain: no gaps found

Status: no issues detected at this verification tier
Elapsed: 520ms
```

A backup this host cannot open — the key is missing, wrong, or the data is
corrupt — fails on the canary before any restore is attempted:

```
Decrypt canary: FAIL (part part_003.tar.br: failed to decrypt/decompress part_003.tar.br: incorrect key)
```

A failed verification (Tier 2 checksum mismatch):

```
Status: CORRUPTED - 1 file(s) failed checksum verification
  MISMATCH: base/1/5678
    stored:   abc123def456...
    computed: 789def012abc...
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All checks passed |
| 1 | One or more checks failed (missing files, corrupt sentinel, checksum mismatch) |
| 2 | Verification could not complete (storage unreachable, auth failure) |

### Caveats and non-guarantees

- Passing `backup-verify` does **not** guarantee that Postgres starts cleanly on
  restore. It narrows the population of restore failures catchable cheaply, but is
  a **complement to periodic full restore tests**, not a replacement.
- For backups taken before SHA256 checksum support (pre-v0.14.2), Tier 2 only
  confirms **readability**, not byte-level integrity.
- WAL chain gaps may appear for old backups if WAL segments have been archived to
  a separate location. Use `--target-lsn` to scope the check.

### Recommended cadence

- **Tier 1**: run hourly or daily as a low-cost health check.
- **Tier 2** (`--sample 5`): run weekly to detect bitrot within cost bounds.
- **Full restore test**: run periodically (monthly) as the ultimate validation.

### Examples

```bash
# Tier 1: verify the latest backup
wal-g backup-verify

# Tier 1: verify a specific backup
wal-g backup-verify base_000000010000000000000001

# Tier 2: spot-check 5% of tar partitions
wal-g backup-verify --sample 5

# Tier 2: verify everything (full scan — uses egress for all parts)
wal-g backup-verify --sample 100

# Reproducible sampling with a specific seed
wal-g backup-verify --sample 10 --seed 42

# WAL chain scoped to a specific LSN
wal-g backup-verify --target-lsn 0/3000000

# JSON output for programmatic consumption
wal-g backup-verify --format json

# Strictly metadata-only: fetch no object data at all
wal-g backup-verify --no-canary
```

## Preflight checks with `doctor`

`backup-verify` answers "is this backup intact?". `wal-g doctor` answers the
question that comes before it: "is this host configured so that a backup and a
restore would work at all?"

```bash
wal-g doctor
```

| Check | What it establishes |
|-------|---------------------|
| `config` | Required settings resolve, and which layer (environment, config file, default/flag) supplied each value |
| `storage` | The credentials in hand can list, write, read back, and delete — not just that the bucket name parses |
| `encryption` | The configured crypter can decrypt what it encrypts, so backups written here are restorable here |
| `postgres` | Connectivity and server version |
| `archiving` | `archive_mode` is on and `archive_command` calls `wal-g wal-push` |
| `backups` | A backup exists in storage, and how old it is |
| `restore-space` | Free space on the data directory against the latest backup's uncompressed size |

Each check reports `pass`, `warn`, `fail`, or `skip`, and every failure carries a
remedy line saying what to do about it. The exit code is 0 when nothing failed and
1 otherwise; **warnings do not affect the exit code**, so a stale backup or an
unencrypted configuration is surfaced without breaking a CI gate.

The storage check writes and then deletes one small probe object. Nothing else is
modified.

```
wal-g doctor

[ OK ] config         2 required setting(s) resolved
       WALG_S3_PREFIX (from environment); AWS_REGION (from config file)
[ OK ] storage        list, write, read, and delete all OK
[WARN] encryption     no encryption configured
       backups are stored unencrypted
       -> Configure WALG_PGP_KEY, WALG_LIBSODIUM_KEY, or a KMS key if the storage is not already encrypted at rest.
[ OK ] postgres       connected to PostgreSQL 16.4 (primary)
[FAIL] archiving      archive_command is not set
       archive_mode is enabled but PostgreSQL has nothing to run, so WAL accumulates in pg_wal instead of reaching storage
       -> Set archive_command = 'wal-g wal-push %p' in postgresql.conf and reload.
[ OK ] backups        7 backup(s), newest is 3h12m old
[ OK ] restore-space  412.8 GiB free, enough to restore the latest backup

5 passed, 1 warned, 1 failed, 0 skipped in 1.84s
NOT ready: 1 check(s) failed.
```

### Options

| Flag | Purpose |
|------|---------|
| `--format text\|json` | Output format. `json` for monitoring or CI pipelines |
| `--skip-pg` | Skip the checks needing a live PostgreSQL connection, for restore-only hosts |
| `--data-dir` | Data directory to size the restore-space check against. Default: `PGDATA` |
| `--space-margin` | Multiple of the backup's uncompressed size that must be free (default 1.2, leaving headroom for WAL replay) |
| `--stale-after` | Age past which the newest backup is reported as stale (default 26h) |

### Recommended use

- On every new host, before the first `backup-push`.
- In CI, after a configuration change to backup settings.
- As the first diagnostic when a backup or restore misbehaves — the output is
  designed to be pasted into a bug report.

```bash
# Restore-only host: no PostgreSQL running
wal-g doctor --skip-pg

# Size a restore against a specific volume
wal-g doctor --data-dir /mnt/restore-target

# Machine-readable, for a monitoring pipeline
wal-g doctor --format json
```
