# WAL-G for PostgreSQL

You can use wal-g as a tool for making encrypted, compressed PostgreSQL backups (full and incremental) and push/fetch them to/from remote storages without saving it on your filesystem.


Configuration
-------------
WAL-G uses [the usual PostgreSQL environment variables](https://www.postgresql.org/docs/current/static/libpq-envars.html) to configure its connection, especially including `PGHOST`, `PGPORT`, `PGUSER`, and `PGPASSWORD`/`PGPASSFILE`/`~/.pgpass`.

`PGHOST` can connect over a UNIX socket. This mode is preferred for localhost connections, set `PGHOST=/var/run/postgresql` to use it. WAL-G will connect over TCP if `PGHOST` is an IP address.

* `WALG_DISK_RATE_LIMIT`

To configure disk read rate limit during ```backup-push``` in bytes per second.

Concurrency values can be configured using:

* `WALG_DOWNLOAD_CONCURRENCY`

To configure how many goroutines to use during ```backup-fetch``` and ```wal-fetch```, use `WALG_DOWNLOAD_CONCURRENCY`. By default, WAL-G uses the minimum of the number of files to extract and 10.

* `WALG_DOWNLOAD_FILE_RETRIES`

To configure how many times failed file will be retried during ```backup-fetch``` and ```wal-fetch```, use `WALG_DOWNLOAD_FILE_RETRIES`. By default is set to 15.

* `WALG_DIRECT_IO`

An experimental feature that allows you to perform direct_io reads during a ```backup-push``` without flushing the disk cache. To activate it, set the value of the environment variable to `true`.

* `WALG_PREFETCH_DIR`

By default WAL prefetch is storing prefetched data in pg_wal directory. This ensures that WAL can be easily moved from prefetch location to actual WAL consumption directory. But it may have negative consequences if you use it with pg_rewind in PostgreSQL 13.
PostgreSQL 13 is able to invoke restore_command during pg_rewind. Prefetched WAL can generate false failure of pg_rewind. To avoid it you can either turn off prefetch during rewind (set WALG_DOWNLOAD_CONCURRENCY = 1) or place wal prefetch folder outside PGDATA. For details see [this pgsql-hackers thread](https://postgr.es/m/CAFh8B=kW8yY3yzA1=-w8BT90ejDoELhU+zho7F7k4J6D_6oPFA@mail.gmail.com).

* `WALG_UPLOAD_CONCURRENCY`

To configure how many concurrency streams to use during backup uploading, use `WALG_UPLOAD_CONCURRENCY`. By default, WAL-G uses 16 streams.

* `WALG_UPLOAD_DISK_CONCURRENCY`

To configure how many concurrency streams are reading disk during ```backup-push```. By default, WAL-G uses 1 stream.

* `TOTAL_BG_UPLOADED_LIMIT` (e.g. `1024`)

Overrides the default `number of WAL files to upload during one scan`. By default, at most 32 WAL files will be uploaded.

* `WALG_SENTINEL_USER_DATA`

This setting allows backup automation tools to add extra information to JSON sentinel file during ```backup-push```. This setting can be used e.g. to give user-defined names to backups. Note: UserData must be a valid JSON string.

* `WALG_PREVENT_WAL_OVERWRITE`

If this setting is specified, during ```wal-push``` WAL-G will check the existence of WAL before uploading it. If the different file is already archived under the same name, WAL-G will return the non-zero exit code to prevent PostgreSQL from removing WAL.

* `WALG_DELTA_MAX_STEPS`

Delta-backup is the difference between previously taken backup and present state. `WALG_DELTA_MAX_STEPS` determines how many delta backups can be between full backups. Defaults to 0 (disabled).
Restoration process will automatically fetch all necessary deltas and base backup and compose valid restored backup (you still need WALs after start of last backup to restore consistent cluster).
Delta computation is based on ModTime of file system and LSN number of pages in datafiles.

Once the limit is reached, the next `backup-push` is **automatically promoted to a full backup**. This fork resolves the chain depth by **walking the increment chain in storage** rather than by trusting the delta count recorded in the base backup's sentinel — see [Delta chain depth and auto-promotion](#delta-chain-depth-and-auto-promotion).

* `WALG_DELTA_ORIGIN`

To configure base for next delta backup (only if `WALG_DELTA_MAX_STEPS` is not exceeded). `WALG_DELTA_ORIGIN` can be LATEST (chaining increments), LATEST_FULL (for bases where volatile part is compact and chaining has no meaning - deltas overwrite each other). Defaults to LATEST.

* `WALG_FORCE_WAL_DELTA`

To prevent WAL-G from falling back to a full scan delta backup when it fails to download delta files.

* `WALG_TAR_SIZE_THRESHOLD`

To configure the size of one backup bundle (in bytes). Smaller size causes granularity and more optimal, faster recovering. It also increases the number of storage requests, so it can costs you much money. Default size is 1 GB (`1 << 30 - 1` bytes).

* `WALG_TAR_DISABLE_FSYNC`

Disable calling fsync after writing files when extracting tar files.

* `WALG_PG_WAL_SIZE`

To configure the wal segment size if different from the postgres default of 16 MB

* `WALG_UPLOAD_WAL_METADATA`

To upload metadata related to wal files. `WALG_UPLOAD_WAL_METADATA` can be INDIVIDUAL (generates metadata for all the wal logs) or BULK( generates metadata for set of wal files) 
Sample metadata file (000000020000000300000071.json)
```bash
{
    "000000020000000300000071": {
    "created_time": "2021-02-23T00:51:14.195209969Z",
    "date_fmt": "%Y-%m-%dT%H:%M:%S.%fZ"
    }
}
```
If the parameter value is NOMETADATA or not specified, it will fallback to default setting (no wal metadata generation)

* `WALG_ALIVE_CHECK_INTERVAL`

To control how frequently WAL-G will check if Postgres is alive during the backup-push. If the check fails, backup-push terminates.

Examples:
- `0` - disable the alive checks
- `1m` - check every 1 minute (default value)
- `10s` - check every 10 seconds
- `10m` - check every 10 minutes


* `WALG_STOP_BACKUP_TIMEOUT`

Timeout for the pg_stop_backup() call. By default, there is no timeout.

Examples:
- `0` - disable the timeout (default value)
- `10s` - 10 seconds timeout
- `10m` - 10 minutes timeout


Usage
-----

### ``backup-fetch``

When fetching base backups, the user should pass in the name of the backup and a path to a directory to extract to. If this directory does not exist, WAL-G will create it and any intermediate subdirectories.

```bash
wal-g backup-fetch ~/extract/to/here example-backup
```

WAL-G can also fetch the latest backup using:

```bash
wal-g backup-fetch ~/extract/to/here LATEST
```

WAL-G can fetch the backup that has the specific UserData (stored in backup metadata) using the `--target-user-data` flag or `WALG_FETCH_TARGET_USER_DATA` variable:
```bash
wal-g backup-fetch /path --target-user-data "{ \"x\": [3], \"y\": 4 }"
```

#### Free-space preflight

Before extracting anything, `backup-fetch` compares the backup's recorded
uncompressed size against the free space on the filesystems it would write to. A
restore that cannot fit is refused in the time it takes to read the sentinel,
rather than failing hours later with a half-written data directory.

```bash
# Require 50% headroom instead of the default 20%
wal-g backup-fetch /path LATEST --space-margin 1.5

# Proceed even though the preflight says it will not fit
wal-g backup-fetch /path LATEST --force

# Skip the check entirely
wal-g backup-fetch /path LATEST --skip-space-check
```

| Flag | Default | Purpose |
|------|---------|---------|
| `--space-margin` | `1.2` | Multiple of the uncompressed size that must be free. The headroom covers WAL replayed during recovery and the cluster accepting writes once it is up |
| `--force` | `false` | Downgrade a refusal to a warning |
| `--skip-space-check` | `false` | Do not check at all |

The check reports one of four verdicts, and only ever refuses on the second:

- **sufficient** — the restore fits, with margin.
- **insufficient** — it demonstrably does not fit. The restore is refused unless `--force`.
- **indeterminate** — the backup uses tablespaces spread across several
  filesystems. A backup sentinel records only a *total* uncompressed size, with no
  per-tablespace breakdown, so how that total divides between those filesystems is
  not knowable before extracting. The restore proceeds with a warning. Only a total
  that cannot fit even when every filesystem is pooled is treated as a refusal.
- **unknown** — the backup predates size recording, so there is nothing to size
  against. The restore proceeds.

Tablespaces that share a filesystem are collapsed into a single pool, so they are
not each credited with the same free space.

The same sizing logic backs `wal-g doctor`'s `restore-space` check, so a
preflight refusal and a doctor failure always agree.

#### Reverse delta unpack

Beta feature: WAL-G can unpack delta backups in reverse order to improve fetch efficiency.

To activate this feature, do one of the following:


* set the `WALG_USE_REVERSE_UNPACK`environment variable
* add the --reverse-unpack flag
```bash
wal-g backup-fetch /path LATEST --reverse-unpack
```

#### Redundant archives skipping

With [reverse delta unpack](#reverse-delta-unpack) turned on, you also can turn on redundant archives skipping.
Since this feature involves both backup creation and restore process, in order to fully enable it you need to do two things:

1. Optional. Increases the chance of archive skipping, but may result in slower backup creation. [Enable rating tar ball composer](#rating-composer-mode) for `backup-push`.

2. Enable redundant backup archives skipping during backup-fetch. Do one of the following:

   * set the `WALG_USE_REVERSE_UNPACK` and `WALG_SKIP_REDUNDANT_TARS` environment variables
   * add the `--reverse-unpack` and `--skip-redundant-tars` flags

```bash  
wal-g backup-fetch /path LATEST --reverse-unpack --skip-redundant-tars
```

#### Partial restore (experimental)

During partial restore wal-g restores only specified databases' files. Use 'database' or 'database/namespace.table' as a parameter ('public' namespace can be omitted).  

```bash  
wal-g backup-fetch /path LATEST --restore-only=my_database,"another database",database/my_table
```

Note: Double quotes are only needed to insert spaces and will be ignored

Example:

`--restore-only=my_db,"another db"`

is equivalent to

`--restore-only=my_db,another" "db`

or even

`--restore-only=my_db,anoth"e"r" "d"b"`



Require files metadata with database names data, which is automatically collected during local backup. With remote backup this option does not work.   

Restores system databases and tables automatically.

Options `--skip-redundant-tars` and `--reverse-unpack` are set automatically.

Because of unrestored databases' or tables remains are still in system tables, it is recommended to drop them.

### ``backup-push``

When uploading backups to storage, the user should pass the Postgres data directory as an argument.

```bash
wal-g backup-push $PGDATA
```
WAL-G will check that command argument, environment variable PGDATA and config setting PGDATA are the same, if set.

If a backup is started from a standby server, WAL-G will monitor the timeline of the server. If a promotion or timeline change occurs during the backup, the data will be uploaded but not finalized, and WAL-G will exit with an error. The logs will contain the necessary information to finalize the backup, which can then be used if you clearly understand the risks.

``backup-push`` can also be run with the ``--permanent`` flag, which will mark the backup as permanent and prevent it from being removed when running ``delete``.

#### Remote backup

WAL-G backup-push allows for two data streaming options:

1. Running directly on the database server as the postgres user, wal-g can read the database files from the filesystem. This option allows for high performance, and extra capabilities, such as partial restore or Delta backups.

   For uploading backups to S3 using streaming option 1, the user should pass in the path containing the backup started by Postgres as in:

   ```bash
   wal-g backup-push /backup/directory/path
   ```

2. Alternatively, WAL-G can stream the backup data through the postgres [BASE_BACKUP protocol](https://www.postgresql.org/docs/current/app-pgbasebackup.html). This allows WAL-G to stream the backup data through the tcp layer, allows to run remote, and allows WAL-G to run as a separate linux user. WAL-G does require a database connection with replication privileges. Do note that the BASE_BACKUP protocol does not allow for multithreaded streaming, and that Delta backup currently is not implemented.

   To stream the backup data, leave out the data directory. And to set the hostname of the postgres server, you can use the environment variable PGHOST, or the WAL-G argument --pghost.

   ```bash
   # Inline
   PGHOST=srv1 wal-g backup-push

   # Export
   export PGHOST=srv1
   wal-g backup-push

   # Use commandline option
   wal-g backup-push --pghost srv1
   ```

The remote backup option can also be used to:

* Run Postgres on multiple hosts (streaming replication), and backup with WAL-G using multihost configuration: ``wal-g backup-push --pghost srv1,srv2``
* Run Postgres on a windows host and backup with WAL-G on a linux host: ``PGHOST=winsrv1 wal-g backup-push``
* Schedule WAL-G as a Kubernetes CronJob

#### Rating composer mode

In the rating composer mode, WAL-G places files with similar updates frequencies in the same tarballs during backup creation. This should increase the effectiveness of `backup-fetch` [redundant archives skipping](#redundant-archives-skipping). Be aware that although rating composer allows saving more data, it may result in slower backup creation compared to the default tarball composer.

To activate this feature, do one of the following:

* set the `WALG_USE_RATING_COMPOSER`environment variable
* add the --rating-composer flag

```bash
wal-g backup-push /path --rating-composer
```

#### Copy composer mode

In the copy composer mode, WAL-G makes a full backup and copies unchanged tar files from previous full backup. In case when there are no previous full backup, `regular` composer is used.

To activate this feature, do one of the following:

* set the `WALG_USE_COPY_COMPOSER`environment variable
* add the --copy-composer flag

```bash
wal-g backup-push /path --copy-composer
```

#### Database composer mode

In the database composer mode, WAL-G separated files from different directories inside default tablespace and packs them in different tars. Designed to increase partial restore performance.

To activate this feature, do one of the following:

* set the `WALG_USE_DATABASE_COMPOSER` environment variable
* add the --database-composer flag

```bash
wal-g backup-push /path --database-composer
```


#### Backup without metadata

By default, WAL-G tracks metadata of the backed up files. If millions of files are backed up (typically in case of hundreds of databases and thousands of tables in each database), tracking this metadata alone would require GBs of memory.

If `--without-files-metadata` or `WALG_WITHOUT_FILES_METADATA` is enabled, WAL-G does not track metadata of the files backed up. This significantly reduces the memory usage on instances with `> 100k` files.

Limitations

* Cannot be used with `rating-composer`, `copy-composer`
* Cannot be used with `WALG_DELTA_MAX_STEPS` setting or `delta-from-user-data`, `delta-from-name` flags.

To activate this feature, do one of the following:

* set the `WALG_WITHOUT_FILES_METADATA`environment variable
* add the `--without-files-metadata` flag

```bash
wal-g backup-push /path --without-files-metadata
```

#### Create delta backup from specific backup
When creating delta backup (`WALG_DELTA_MAX_STEPS` > 0), WAL-G uses the latest backup as the base by default. This behaviour can be changed via following flags:

* `--delta-from-name` flag or `WALG_DELTA_FROM_NAME` environment variable to choose the backup with specified name as the base for the delta backup

* `--delta-from-user-data` flag or `WALG_DELTA_FROM_USER_DATA` environment variable to choose the backup with specified user data as the base for the delta backup

Examples:
```bash
wal-g backup-push /path --delta-from-name base_000000010000000100000072_D_000000010000000100000063
wal-g backup-push /path --delta-from-user-data "{ \"x\": [3], \"y\": 4 }"
```

When using the above flags in combination with `WALG_DELTA_ORIGIN` setting, `WALG_DELTA_ORIGIN` logic applies to the specified backup. For example:
```bash
list of backups in storage:
base_000000010000000100000040  # full backup
base_000000010000000100000046_D_000000010000000100000040  # 1st delta
base_000000010000000100000061_D_000000010000000100000046  # 2nd delta
base_000000010000000100000070  # full backup

export WALG_DELTA_ORIGIN=LATEST_FULL
wal-g backup-push /path --delta-from-name base_000000010000000100000046_D_000000010000000100000040

wal-g logs:
INFO: Selecting the backup with name base_000000010000000100000046_D_000000010000000100000040 as the base for the current delta backup...
INFO: Delta will be made from full backup.
INFO: Delta backup from base_000000010000000100000040 with LSN 140000060.
```

#### Delta chain depth and auto-promotion

When the chain reaches `WALG_DELTA_MAX_STEPS`, the next `backup-push` is taken as a full backup instead of another delta. That much is upstream behaviour. What this fork changes is **how the depth is established**.

Upstream reads the delta count out of the base backup's sentinel and adds one. That count is written once and never revisited, so when it is missing or wrong — an older backup, a partially written sentinel, a backup copied in from another storage — counting restarts from one and the chain keeps growing silently past its limit. A restore then has to apply every link in a chain nobody knew was that long.

This fork walks the increment chain in storage instead, following each link back to the full backup at its base. The walk costs one sentinel read per link, once per `backup-push`, and cannot drift from what is actually there. When the walked depth disagrees with the recorded count, the walked depth is what the limit is applied to, and the disagreement is logged:

```
WARNING: base_...17 records a delta count of 1 but sits 4 link(s) into a chain in storage.
         Using the depth found in storage, so the limit is applied to the real chain.
```

Promotion to a full backup happens for these reasons, which are recorded on the resulting backup's sentinel as `DeltaPromotionReason`:

| Reason | Meaning |
| --- | --- |
| `chain_at_max_depth` | The chain has reached `WALG_DELTA_MAX_STEPS` |
| `chain_broken` | A backup the chain depends on is not in storage |
| `chain_cycle` | The chain's links refer back to each other |
| `chain_unreadable` | A link's sentinel could not be read, or names an increment base without the rest of its increment fields, so the true depth is unknown |
| `base_is_permanent` | The base is a permanent backup, and a delta on it would pin it in place |
| `base_without_lsn` | The base predates the delta feature and records no start LSN |

The reason is written only when a delta was actually possible and was declined. Full backups taken because deltas are switched off, or because there is no previous backup, carry no reason — recording those would put a field on nearly every full backup and bury the cases worth auditing.

Because a broken or unreadable chain promotes rather than extends, a corrupt link cannot be built on top of. That also keeps an inconsistent sentinel away from the increment-handling code, which panics on one.

What the limit counts depends on `WALG_DELTA_ORIGIN`:

- **LATEST** (default) chains each delta onto the previous one, so the chain deepens and a restore must apply every link. The depth walked from storage is what the limit bounds.
- **LATEST_FULL** rebases every delta onto the full backup, so the chain is never deeper than one link and a walked depth could never reach the limit. There the limit bounds how many deltas accumulate on a single full backup — which is what keeps that full backup from getting arbitrarily stale — and that count comes from the base's own sentinel. The walk still applies: a broken or unreadable chain promotes in either mode.

#### Page checksums verification
To enable verification of the page checksums during the backup-push, use the `--verify` flag or set the `WALG_VERIFY_PAGE_CHECKSUMS` env variable. If found any, corrupted block numbers (currently no more than 10 of them) will be recorded to the backup sentinel json, for example:
```json
...
"/base/13690/13535": {
"IsSkipped": true,
"MTime": "2020-08-20T21:02:56.690095409+05:00",
"IsIncremented": false
},
"/base/16384/16397": {
"CorruptBlocks": [
1
],
"IsIncremented": false,
"IsSkipped": false,
"MTime": "2020-08-21T19:09:52.966149937+05:00"
},
...
```

### ``wal-fetch``

When fetching WAL archives from S3, the user should pass in the archive name and the name of the file to download to. This file should not exist as WAL-G will create it for you.

WAL-G will also prefetch WAL files ahead of the asked WAL file. These files will be cached in `./.wal-g/prefetch` directory. Cached files older than the recently asked WAL file will be deleted from the cache, to prevent cache bloating. If a cached file is requested with `wal-fetch`, this will also remove it from the cache, but trigger caching of the new file.

```bash
wal-g wal-fetch example-archive new-file-name
```

This command is intended to be executed from the Postgres [restore_command](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-RESTORE-COMMAND) parameter.

Note: ``wal-fetch`` will exit with errorcode 74 (`EX_IOERR: input/output error, see sysexits.h for more info`) if the WAL-file is not available in the repository.
All other errors end in exit code 1, and should stop PostgreSQL rather than ending PostgreSQL recovery.
For PostgreSQL that should be any error code between 126 and 255, which can be achieved with a simple wrapper script.
Please see https://github.com/wal-g/wal-g/pull/1195 for more information.

### ``wal-push``

When uploading WAL archives to S3, the user should pass in the absolute path to where the archive is located.

```bash
wal-g wal-push /path/to/archive
```

This command is intended to be executed from the Postgres [archive_command](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-COMMAND) parameter.

### ``wal-show``

Show information about the WAL storage folder. `wal-show` shows all WAL segment timelines available in storage, displays the available backups for them, and checks them for missing segments.

* if there are no gaps (missing segments) in the range, final status is `OK`
* if there are some missing segments found, final status is `LOST_SEGMENTS`

```bash
wal-g wal-show
```

By default, `wal-show` shows available backups for each timeline. To turn it off, add the `--without-backups` flag.

By default, `wal-show` output is plaintext table. For detailed JSON output, add the `--detailed-json` flag.

### ``wal-verify``

Run series of checks to ensure that WAL segment storage is healthy. Available checks:

#### `integrity`
Ensure that there is a consistent WAL segment history for the cluster so WAL-G can perform a PITR for the backup. Essentially, it checks that all the WAL segments in the range `[oldest backup start segment, current cluster segment)` are available in storage. If no backups found, `[1, current cluster segment)` range will be scanned.
Additionally, you can specify your own timeline and LSN using the `--timeline` and `--lsn` flags. This is useful, for example, to ensure that a replica can be restored from the archive.
You can also specify the name of a particular backup to check using the --backup-name flag. In this case, it checks that all the WAL segments in the range `[backup with specified name start segment, current cluster segment)`."

![SegmentStatusIllustration](resources/wal_verify_segment_statuses.png)

In `integrity` check output, there are four statuses of WAL segments:

* `FOUND` segments are present in WAL storage
* `MISSING_DELAYED` segments are not present in WAL storage, but probably Postgres did not try to archive them via `archive_command` yet
* `MISSING_UPLOADING` segments are the segments which are not present in WAL storage, but looks like that they are in the process of uploading to storage
* `MISSING_LOST` segments are not present in WAL storage and not `MISSING_UPLOADING` nor `MISSING_DELAYED`

`ProbablyUploading` segments range size is taken from `WALG_UPLOAD_CONCURRENCY` setting.

`ProbablyDelayed` segments range size is controlled via `WALG_INTEGRITY_MAX_DELAYED_WALS` setting.  

Output consists of:

1. Status of `integrity` check:
    * `OK` if there are no missing segments 
    * `WARNING` if there are some missing segments, but they are not `MISSING_LOST` 
    * `FAILURE` if there are some `MISSING_LOST` segments
2. A list that shows WAL segments in chronological order grouped by timeline and status.

#### `timeline`
Check if the current cluster timeline is greater than or equal to any of the storage WAL segments timelines. This check is useful to detect split-brain conflicts. Please note that this check works correctly only if new storage created, or the existing one cleaned when restoring from the backup or performing `pg_upgrade`.

Output consists of:

1. Status of `timeline` check:
    * `OK` if current timeline id matches the highest timeline id found in storage
    * `WARNING` if could not determine if current timeline matches the highest in storage
    * `FAILURE` if current timeline id is not equal to the highest timeline id found in storage
2. Current timeline id.
3. The highest timeline id found in WAL storage folder.

Usage:
```bash
wal-g wal-verify [space separated list of checks]
# For example:
wal-g wal-verify integrity timeline # perform integrity and timeline checks
wal-g wal-verify integrity # perform only integrity check
```

By default, `wal-verify` output is plaintext. To enable JSON output, add the `--json` flag.

Example of the plaintext output:
```bash
[wal-verify] integrity check status: OK
[wal-verify] integrity check details:
+-----+--------------------------+--------------------------+----------------+--------+
| TLI | START                    | END                      | SEGMENTS COUNT | STATUS |
+-----+--------------------------+--------------------------+----------------+--------+
|   3 | 00000003000000030000004D | 0000000300000004000000F0 |            420 |  FOUND |
|   4 | 0000000400000004000000F1 | 000000040000000800000034 |            836 |  FOUND |
+-----+--------------------------+--------------------------+----------------+--------+
[wal-verify] timeline check status: OK
[wal-verify] timeline check details:
Highest timeline found in storage: 4
Current cluster timeline: 4
```

Example of the JSON output:
```bash
{
   "integrity":{
      "status":"OK",
      "details":[
         {
            "timeline_id":3,
            "start_segment":"00000003000000030000004D",
            "end_segment":"0000000300000004000000F0",
            "segments_count":420,
            "status":"FOUND"
         },
         {
            "timeline_id":4,
            "start_segment":"0000000400000004000000F1",
            "end_segment":"000000040000000800000034",
            "segments_count":836,
            "status":"FOUND"
         }
      ]
   },
   "timeline":{
      "status":"OK",
      "details":{
         "current_timeline_id":4,
         "highest_storage_timeline_id":4
      }
   }
}
```

### ``wal-receive``

Receive WAL stream using PostgreSQL [streaming replication](https://www.postgresql.org/docs/current/warm-standby.html#STREAMING-REPLICATION) and push to the storage.

You can set `WALG_SLOTNAME` variable to define the [replication slot](https://www.postgresql.org/docs/current/warm-standby.html#STREAMING-REPLICATION-SLOTS) name to be used (defaults to `walg`). The slot name can only consist of the following characters: [0-9A-Za-z_].
When uploading WAL archives to S3, the user should pass in the absolute path to where the archive is located.

```bash
wal-g wal-receive
```


### ``backup-mark``

Backups can be marked as permanent to prevent them from being removed when running ``delete``. Backup permanence can be altered via this command by passing in the name of the backup (retrievable via `wal-g backup-list --pretty --detail --json`), which will mark the named backup and all previous related backups as permanent. The reverse is also possible by providing the `-i` flag.

```bash
wal-g backup-mark example-backup -i
```


### ``catchup-push``

To create a catchup incremental backup, the user should pass the path to the master Postgres directory and the LSN of the replica
for which the backup is created.

Steps:
1) Stop replica
2) Get replica LSN (for example using pg_controldata command)
3) Start uploading incremental backup on master.

``` bash
wal-g catchup-push /path/to/master/postgres --from-lsn replica_lsn
```


### ``catchup-fetch``

To accept catchup incremental backup created by `catchup-push`, the user should pass the path to the replica Postgres
directory and name of the backup.

``` bash
wal-g catchup-fetch /path/to/replica/postgres backup_name
```


### ``catchup-send`` and ``catchup-recieve``

These commands are used in conjunction to catchup lagging replica. On a standby you should run ``catchup-recieve``, then on a primary ``catchup-send``. Standby Postgres must be stopped during this procedure.

``` bash
wal-g catchup-receive ${PGDATA_STANDBY} 1337 &

wal-g catchup-send ${PGDATA_PRIMARY} hostname:1337
```


### ``copy``

This command will help to change the storage and move the set of backups there or write the backups on magnetic tape. For example, `wal-g copy --from=config_from.json --to=config_to.json` will copy all backups.

Flags:

- `-b, --backup-name string` Copy specific backup
- `-f, --from string` Storage config from where should copy backup
- `-t, --to string` Storage config to where should copy backup
- `-w, --with-history` If set - copy WALs older than backup finish_lsn. If not - copy only WALs from start_lsn to finish_lsn

### Delete retention ordering and ``--use-sentinel-time``

For PostgreSQL, ``wal-g delete retain`` and ``wal-g delete before`` sort backups using the **timeline** and WAL **segment number** parsed from the backup name (``base_<timeline>...``) unless you change that behavior.

After a **major version upgrade** (for example ``pg_upgrade``) or a new data directory, the cluster timeline often **starts again at a low value** while backups from the old cluster remain in the same bucket or prefix. Then name-based order is **not** the same as real-world time: ``retain`` may keep old backups and delete **new** ones even when you meant to keep the latest *N* backups. See [issue #636](https://github.com/wal-g/wal-g/issues/636).

Use the ``--use-sentinel-time`` flag on ``wal-g delete`` so WAL-G orders backups by **start time** from each backup's sentinel/metadata (when metadata is present for backups in storage). If metadata cannot be read for some backups, WAL-G **falls back** to timeline and segment ordering.

**Mitigations:** prefer a **separate storage path or bucket** per major PostgreSQL version so pre- and post-upgrade backups are not mixed; always run without ``--confirm`` first to dry-run.

```bash
wal-g delete retain FULL 5 --use-sentinel-time --confirm
```

### ``delete --explain``

Every ``delete`` subcommand accepts ``--explain``. It reports what the delete would remove **and what could still be recovered afterwards**, then exits without deleting anything.

Running ``delete`` without ``--confirm`` already tells you how many objects match. That number does not answer the question that actually matters before a retention change: *how far back will I still be able to restore?* ``--explain`` answers it by computing the recovery window twice — once against storage as it stands, once against storage with the planned objects removed — and reporting the difference.

```bash
wal-g delete retain 7 --explain
```

```
wal-g delete retain 7 --explain

Would delete 21 object(s), 22.2 KiB
  backups      1 deleted, 1 retained
  WAL segments 18

Backups to delete
  2026-08-01T00:05:00Z  base_000000010000000000000002

Backups to keep
  2026-08-02T00:05:00Z  base_000000010000000000000014

Recovery window
  before 2026-08-01T00:05:00Z .. 2026-08-03T14:21:51Z  (2d14h recoverable across 1 window(s))
  after  2026-08-02T00:05:00Z .. 2026-08-03T14:21:51Z  (1d14h recoverable across 1 window(s))

       Reclaims 22.2 KiB across 21 object(s): 1 backup(s) and 18 WAL segment(s).
       The earliest restorable point moves forward from 2026-08-01T00:05:00Z to 2026-08-02T00:05:00Z, giving up 1d0h of history.
       Recoverable time goes from 2d14h to 1d14h.

Nothing was deleted. Re-run with --confirm to execute.
```

Lines starting with ``[WARN]`` mark consequences that are usually not intended:

- the delete leaves **nothing** restorable;
- the **latest** restorable point moves backwards, meaning recently archived WAL is in scope;
- a new **gap** opens in the recovery window, so the remaining range is no longer recoverable end to end;
- backups are **retained but become unrestorable** — a delta whose base is deleted, or a backup whose WAL is going. These keep costing storage and keep appearing in ``backup-list`` while being unable to restore anything;
- a **permanent** backup is in scope.

Trimming the *old* end of the window is what a retention delete is for, so it is reported as an effect rather than a warning.

``--explain`` and ``--confirm`` cannot be combined: they ask for opposite things, and guessing wrong in one direction is not recoverable. Use ``--format json`` to consume the report from a pipeline.

The scope shown is computed by the same handler, arguments and filters as the real delete, with deletion swapped for collection at the last step, so an explained delete cannot disagree with the delete it describes.

### ``pitr-window``

Reports the ranges of time the storage can currently be restored to.

```bash
wal-g pitr-window
```

```
wal-g pitr-window

Recoverable  2026-08-01T00:05:00Z .. 2026-08-03T14:21:51Z  (2d14h across 1 window(s))

  timeline 1  2026-08-01T00:05:00Z .. 2026-08-03T14:21:51Z  (2d14h)
              WAL 000000010000000000000002 .. 000000010000000000000028, 2 backup(s)

2 of 2 backup(s) can serve a restore.
```

A restore needs a backup that can reach a consistent state and an unbroken run of WAL from it. So a window opens when a backup **finishes** — recovery cannot target a moment part-way through a backup — and runs until the first missing WAL segment after it. Overlapping windows merge; what is left between them is a **gap**, a stretch of history that no backup in storage can recover to.

Windows are reported per timeline. A restore can follow a timeline switch, but which fork a given moment belongs to depends on the recovery target, so folding timelines together would report windows that no single restore could deliver.

Window ends are dated from the **upload time** of the last WAL segment, which approximates the commit times inside it. Reading the actual commit timestamps would mean fetching and decrypting every segment; the upload time is close enough to size a recovery window and costs one listing.

Backups that cannot serve a restore at all are listed separately, with the reason: a delta whose base is gone (``broken_increment_chain``), or a backup missing the WAL it needs to reach consistency (``missing_own_wal``).

Flags:

- ``--format`` Output format: ``text`` or ``json``. Default: ``text``.
- ``--min-window`` Exit non-zero if less than this much recoverable time remains, e.g. ``72h``.

The exit code is 1 when nothing is restorable, or when ``--min-window`` is set and the recoverable span falls short of it; 0 otherwise. That makes it usable as a CI or cron gate against a retention policy that has quietly stopped covering its stated RPO:

```bash
wal-g pitr-window --min-window 72h || alert "PITR window under 3 days"
```

### ``retention-validate``

Checks that the retention policy you run actually delivers the recovery objectives you claim.

```bash
wal-g retention-validate --rpo 1h --retention-window 30d --retain 36
```

Two questions are asked, and they fail independently:

- Does storage meet the objectives **right now**?
- Would it still meet them **once the declared retention policy has been applied**?

A storage that passes the first and fails the second is the case this command exists for. It looks healthy only because the policy has not caught up with it yet — the backups that satisfy the window today are the ones tonight's `delete retain` is about to remove.

The second question is answered by running the declared policy through the **same delete handler `delete retain` uses**, with deletion swapped for collection. What is validated is the delete that would actually happen, not a model of it. Nothing is deleted.

```
wal-g retention-validate

Declared  RPO 1d0h, retention window 2d0h, retain 1

[ OK ] rpo              1h13m of data at risk, within the 1d0h RPO
       newest restorable point is 2026-08-03T14:21:51Z, 1h13m ago
[ OK ] retention-window Storage is continuously restorable back to 2026-08-01T00:05:00Z, covering the 2d0h required
[FAIL] policy-outcome   after `delete retain 1`, storage reaches back to 2026-08-02T00:05:00Z, 8h29m short of the 2d0h required
       a restore to any point before 2026-08-02T00:05:00Z is not possible
       -> Retaining 1 backup(s) does not sustain the declared window. Raise the retain count, or lower the declared window to what the policy can keep.
[WARN] backup-cadence   retaining 1 backup(s) leaves no window at all between backups
       -> Retain at least two backups so a window exists between the oldest and newest.

2 passed, 1 warned, 1 failed, 0 skipped
Objectives NOT met: 1 check(s) failed.
```

| Check | What it asks |
| --- | --- |
| `rpo` | How much data a failure right now would lose: the distance from the newest restorable point to now. Catches stalled WAL archiving. |
| `retention-window` | Whether the required period is **continuously** restorable. A total of recoverable hours is not enough — the same total can be one window or several with holes between them, and only one of those is a retention window. |
| `policy-outcome` | Whether it still would be after the policy runs. |
| `backup-cadence` | Whether the policy can **keep** meeting the window at the observed backup cadence, rather than only today. Retaining N backups holds a window about (N−1) intervals wide; if that is short of the declared window, the policy is guaranteed to fail eventually even while storage currently passes. |

The window checks end at the newest restorable point rather than at now, because the distance from there to now is what `rpo` measures. Judging it twice would report one problem as two. Gaps older than the declared window are ignored — they are real, and `pitr-window` reports them, but they are not this policy's problem.

Objectives may be declared as flags or as settings (`WALG_RPO`, `WALG_RETENTION_WINDOW`, `WALG_RETENTION_COUNT`), so a cron job and a CI gate can be judged against the same numbers. An objective that is not declared is **skipped, not passed** — and a report where everything was skipped says so rather than claiming the objectives were met.

The exit code is 0 when nothing failed and 1 otherwise; warnings do not affect it.

```bash
# Fails the build if the policy cannot deliver what it claims
wal-g retention-validate --rpo 1h --retention-window 30d --retain 36 --format json
```

### ``restore-test``

Rehearses a restore for real, times it, and judges it against the recovery objectives you declare. A backup that has never been restored is a backup nobody has tested.

```bash
wal-g restore-test --target-dir /mnt/drill --rto 2h --rpo 1h
```

```
wal-g restore-test

Restoring  base_000000010000000000000014 into /mnt/drill

[ OK ] target-dir   /mnt/drill is empty and is not the live data directory
[ OK ] space        412 GiB free for a restore of about 168 GiB
[ OK ] fetch        168 GiB restored in 41m18s
       69 MiB/s
[SKIP] replay       skipped (--start-postgres not given)
[ OK ] rto          recovery took 41m18s of the 2h0m budget (fetch only)
       -> Replay time is NOT included. Pass --start-postgres to measure it.
[ OK ] rpo          restorable to within 8m, inside the 1h0m RPO

4 passed, 0 warned, 0 failed, 1 skipped
Drill passed. Scratch directory removed.
```

By default the drill measures the **fetch only**, and the `rto` phase says so rather than letting a pass read as a full recovery rehearsal. Pass `--start-postgres` to also start the restored cluster on a scratch port (5433 by default), replay WAL to consistency, and include that in the measured time. That needs `pg_ctl`, found via `--pg-ctl` or `PATH`. Recovery configuration is written the way the restored cluster's version expects — `recovery.signal` plus `postgresql.conf` for PostgreSQL 12 and later, `recovery.conf` before it.

**Safety.** The drill writes a whole cluster and then deletes it, so the target is checked before anything happens:

- the target directory must **not** be `PGDATA`, or inside it;
- it must be **empty or absent** — the drill refuses to restore over existing files;
- what the drill creates, it removes, unless `--keep` is given. A directory that already existed is emptied rather than removed.

The restore runs as a **separate wal-g process**. A restore that fails hard calls `os.Exit`, and a drill that died without a verdict — leaving the half-written cluster behind — would be a poor test harness. Running the documented command also means the drill exercises the path an operator would actually use, config resolution included. A failed restore is reported with the real error:

```
[FAIL] fetch        the restore failed
       ERROR: Failed to fetch backup: Expect pg_control archive, but not found
       -> This is the failure a real restore would hit. Run `wal-g backup-fetch` directly for the full output.
```

Flags:

- `--target-dir` (required) Directory to restore into.
- `--rto`, `--rpo` Budgets to judge against. Default to `WALG_RTO` and `WALG_RPO`.
- `--start-postgres` Start the restored cluster and measure replay.
- `--pg-ctl`, `--port`, `--start-timeout` Control that cluster.
- `--keep` Leave the restored cluster in place for inspection.
- `--wal-g-binary` The wal-g to restore with. Defaults to this binary, so a drill rehearses the build that is deployed.
- `--format` `text` or `json`.

The exit code is 0 when nothing failed and 1 otherwise, which makes it usable from cron as a standing rehearsal:

```bash
wal-g restore-test --target-dir /mnt/drill --rto 2h --format json || alert "restore drill failed"
```

### ``delete garbage``

Deletes outdated WAL archives and backups leftover files from storage, e.g. unsuccessfully backups or partially deleted ones. Will remove all non-permanent objects before the earliest non-permanent backup. This command is useful when backups are being deleted by the `delete target` command.

If there are no non-permanent backup in storage, command won`t delete anything. To bypass this check, and delete garbage anyway, use ``--without-backup-check`` flag.

Usage:
```bash
wal-g delete garbage           # Deletes outdated WAL archives and leftover backups files from storage
wal-g delete garbage ARCHIVES      # Deletes only outdated WAL archives from storage
wal-g delete garbage BACKUPS       # Deletes only leftover (partially deleted or unsuccessful) backups files from storage
```

The `garbage` target can be used in addition to the other targets, which are common for all storages.

### ``wal-restore``

Restores the missing WAL segments that will be needed to perform pg_rewind from storage. The current version supports only local clusters.

Usage:
```bash
wal-g wal-restore path/to/target-pgdata path/to/source-pgdata
```

### ``daemon``

Long-running process that archives and fetches WAL segments in response to commands sent over a UNIX socket. The daemon stays warm so PostgreSQL avoids paying WAL-G's startup cost (config reload, storage connect) once per WAL segment.

Usage:
```bash
wal-g daemon path/to/socket
```

Configuration:

* `WALG_DAEMON_WAL_UPLOAD_TIMEOUT`

Per-archive operation time limit. Operations exceeding it are interrupted. Default `60s`.

##### ``walg-daemon-client``

Lightweight CLI in [`cmd/daemonclient`](https://github.com/wal-g/wal-g/tree/master/cmd/daemonclient), built via `make build_client`. Intended to be invoked from [`archive_command`](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-COMMAND) and [`restore_command`](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-RESTORE-COMMAND), so PostgreSQL forks the small client per segment instead of the full `wal-g` binary.

Usage:
```bash
walg-daemon-client socket command [command_args] [-timeout duration] [-connection-timeout duration]
```

Commands:
- `wal-push wal_filepath` — relays to `wal-g wal-push`
- `wal-fetch wal_name destination_filename` — relays to `wal-g wal-fetch`. On a missing archive, exits `74` (`EX_IOERR`) so PostgreSQL keeps recovering rather than treating it as fatal; matches `wal-fetch` behaviour, see [PR #1195](https://github.com/wal-g/wal-g/pull/1195).

`postgresql.conf` example:
```conf
archive_mode = on
archive_command = 'walg-daemon-client /var/run/wal-g.sock wal-push %f'
restore_command = 'walg-daemon-client /var/run/wal-g.sock wal-fetch %f %p'
```

##### ``walg_archive``

PostgreSQL extension hosted at https://github.com/wal-g/walg_archive. Targets PostgreSQL 15+ via the [`archive_library`](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-LIBRARY) GUC, replacing `archive_command` shell invocation with an in-process callback. Build it from that repo, install the resulting `.so`, then in `postgresql.conf`:
```conf
archive_mode = on
archive_library = 'walg_archive'
walg_archive.walg_socket = '/var/run/wal-g.sock'
```
`walg_archive.walg_socket` must point at the same path passed to `wal-g daemon`.

pgBackRest backups support (beta version)
-----------
### ``pgbackrest backup-list``

List pgbackrest backups.

Usage:
```bash
wal-g pgbackrest backup-list [--pretty] [--json] [--detail]
```

### ``pgbackrest backup-fetch``

Fetch pgbackrest backup. For now works only with full backups, incr and diff backups are not supported.

Usage:
```bash
wal-g pgbackrest backup-fetch path/to/destination-directory backup-name
```

### ``pgbackrest wal-fetch``

Fetch wal file from pgbackrest backup

Usage:
```bash
wal-g pgbackrest wal-fetch example-archive new-file-name
```

### ``pgbackrest wal-show``

Show wal files from pgbackrest backup

Usage:
```bash
wal-g pgbackrest wal-show
```

[Information about failover storages configuration](FailoverStorages.md)

Playground
-----------
If you prefer to use a Docker image, you can directly test WAL-G with this [playground](https://github.com/stephane-klein/playground-postgresql-walg).

Please note, that is a third-party repository, and we are not responsible for it to always work correctly.
