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

## How far back can I recover? `pitr-window`

`backup-verify` answers whether a backup is intact and `doctor` answers whether
this host could use it. Neither answers the question a retention policy is
actually judged by: **which moments in time can still be restored to.**

```bash
wal-g pitr-window
```

A restore needs a backup that can reach a consistent state and an unbroken run
of WAL from it, so a window opens when a backup *finishes* and runs until the
first missing WAL segment after it. Overlapping windows merge; what is left
between them is a gap — a stretch of history nothing in storage can recover to.
Backups that can no longer serve a restore at all, because their base is gone or
their WAL is missing, are listed separately: they still cost storage and still
appear in `backup-list`.

The exit code is 1 when nothing is restorable, or when the recoverable span is
shorter than `--min-window`, which makes it a gate on the RPO you claim to meet:

```bash
# Fails if less than three days of history remains recoverable
wal-g pitr-window --min-window 72h
```

Before changing a retention policy, `wal-g delete ... --explain` reports the same
window computed before and after the delete, and warns when the delete would
leave nothing restorable, open a gap, or strand backups in storage. Full
documentation for both: [PostgreSQL.md](PostgreSQL.md#pitr-window).

## Does the policy deliver the RPO? `retention-validate`

`pitr-window` reports what the storage can recover to. It does not know what you
promised. `retention-validate` takes the objectives you declare — an RPO, a
retention window, and the backup count your policy keeps — and checks both
whether storage meets them now and whether it still would once the policy has
run:

```bash
wal-g retention-validate --rpo 1h --retention-window 30d --retain 36
```

The second half is the point. A storage can satisfy a 30-day window today purely
because backups nobody has pruned yet are still there; the policy that runs
tonight is what decides whether it still will tomorrow. The declared policy is
run through the same delete handler `delete retain` uses, so the window being
validated is the one the real delete would leave. Nothing is deleted.

The `backup-cadence` check goes one step further and asks whether the policy can
*keep* meeting the window at the observed backup cadence, which fails on a
misconfiguration before storage has had a chance to show it.

Objectives can be set as `WALG_RPO`, `WALG_RETENTION_WINDOW` and
`WALG_RETENTION_COUNT` so a cron job and a CI gate are judged against the same
numbers. Exit code 1 when any check fails. Full documentation:
[PostgreSQL.md](PostgreSQL.md#retention-validate).

## Rehearsing the restore: `restore-test`

Everything above reads metadata. `backup-verify` samples the objects,
`retention-validate` reasons about windows, `doctor` checks the host. None of
them restore anything, and a backup that has never been restored is a backup
nobody has tested.

```bash
wal-g restore-test --target-dir /mnt/drill --rto 2h --rpo 1h
```

The drill restores a backup for real into a scratch directory, times it, judges
the elapsed time against the declared RTO and the reachable recovery point
against the RPO, and then removes what it created.

By default it measures the fetch only, and says so — an RTO pass that silently
omitted WAL replay would be exactly the kind of false assurance this fork tries
not to give. `--start-postgres` starts the restored cluster on a scratch port,
replays to consistency, and folds that into the measured time.

The safety rules are absolute: never `PGDATA` or anything inside it, never a
directory that already has files in it, and clean up afterwards unless `--keep`
says otherwise.

### Recommended cadence

- Monthly, or after any change to compression, encryption or storage layout.
- From cron with `--format json`, so a drill that stops passing is noticed
  before the day it matters.
- With `--start-postgres` at least occasionally, since replay time is the part
  of an RTO that fetch throughput cannot predict.

Full documentation: [PostgreSQL.md](PostgreSQL.md#restore-test).

## Evidence for an audit: `compliance-report`

`doctor`, `backup-verify`, `retention-validate`, `pitr-window`, and
`restore-test` each answer one question on their own. `compliance-report` runs
them and collects the answers into a single artifact — something to hand to
an auditor or attach to a change record, rather than five separate command
outputs to reassemble by hand.

```bash
# Default: doctor, backup-verify (Tier 1, LATEST), retention-validate,
# pitr-window. restore-test is skipped (see below).
wal-g compliance-report --format json

# Include a real restore rehearsal in the evidence:
wal-g compliance-report --format json --restore-test-target-dir /mnt/drill
```

**This is an evidence aggregator, not a certified compliance report.** It
does not produce a SOC2 report — that requires an independent auditor's
opinion — or a CMMC assessment — that requires a formal assessor. Each check
is tagged with illustrative categories (e.g. "backup integrity", "data
retention") to help orient a reader; these are starting points, not a vetted
mapping to specific CMMC practices or SOC2 Trust Services Criteria. Review
them with your compliance team before citing this report in an audit.

Each check runs as its own `wal-g` process — the same invocation an operator
would type by hand — with `--format json`. Its full JSON report is embedded
verbatim under `detail`, so the report is complete evidence, not a lossy
summary. Pass/fail per check comes from that process's exit code: 0 → `pass`,
1 → `fail`; `backup-verify`'s documented exit code 2 ("could not complete" —
storage unreachable, auth failure) is reported as its own `error` status
rather than folded into `fail`.

| Flag | Forwards to | Default |
|---|---|---|
| `--restore-test-target-dir` | `restore-test --target-dir` | unset — `restore-test` is `skipped` |
| `--backup-name` | `backup-verify <name>` | unset — `backup-verify` checks `LATEST` |
| `--sample` | `backup-verify --sample` | unset — Tier 1 only |
| `--min-window` | `pitr-window --min-window` | unset |

`retention-validate` is run with no flags forwarded: it already resolves
`WALG_RPO`/`WALG_RETENTION_WINDOW`/`WALG_RETENTION_COUNT` from configuration
on its own, so a plain `retention-validate --format json` reflects the same
policy a cron job or CI gate would validate against.

The exit code is 0 when every check that ran passed, 1 otherwise. A `skipped`
check (currently only `restore-test`, when no target directory is given) does
not affect it — the same rule `doctor` and `retention-validate` already use
for warnings.
