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
WAL chain: no gaps found

Status: no issues detected at this verification tier
Elapsed: 520ms
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
```
