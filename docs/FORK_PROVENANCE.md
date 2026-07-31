# Fork Provenance

This document records how the upstream of this repository was determined and
corrects the earlier, incorrect attribution. It is an audit-trail record:
everything below was verified on 2026-07-31 from repository-internal
evidence only.

## Correction notice

Earlier versions of this repository's README and NOTICE attributed the
upstream project to "golang-migrate/wal-g" with copyright to "golang-migrate
contributors". That attribution was incorrect: golang-migrate is an unrelated
database schema migration library, and no golang-migrate code, history, or
submodules exist in this repository. The attribution was corrected in
July 2026 (PR for branch `fix/upstream-attribution`).

The "golang-migrate/wal-g" string originated solely from this fork's own
integration commit message (`7adf4e22`, "feat: integrate original WAL-G
codebase from golang-migrate/wal-g", 2026-06-06) and was copied from there
into README.md and NOTICE. It does not appear anywhere in the inherited
codebase or its history.

## Verified upstream

- **Upstream repository:** https://github.com/wal-g/wal-g
- **Upstream license:** Apache License 2.0, "Copyright 2017 Citus Data Inc."
  (as recorded in upstream LICENSE.md, preserved unchanged in this repository)
- **Fork point commit:** `7e9f90554506c260d08e521b350d0df306062a9e`
  ("CI cleanup: remove go get (#2269)", Philip Dubé, 2026-06-04) — the tip of
  the imported upstream history, merged into this repository on 2026-06-06
  by merge commit `4b521c63`.
- **Upstream version at fork:** not determinable from repository evidence.
  Upstream release tags are not present in this repository; the fork's own
  tags (`v0.14.1`, `v0.14.2-lts.1`, `v0.14.3-stable`, `v0.15.0-stable`,
  `v0.15.1`) are fork-created and `v0.14.1` is not an upstream release tag.
  The fork point is therefore anchored by commit SHA, commit date, and the
  final upstream pull request present in the imported history (#2269).

## Evidence and method

1. **Module identity.** `git log -p --follow go.mod` shows the Go module was
   `github.com/wal-g/wal-g` before the fork renamed it to
   `github.com/lateos-ai/wal-g` (fork commit `085cffac`).
2. **History root.** The repository's root commit is `7c4dcf3c` ("File and
   NOP", 2017-06-19, Katie Li, Citus Data). Full upstream history (2,365
   commits) is present; it was not squashed or rewritten.
3. **License header.** Upstream `LICENSE.md` at the fork point reads
   "Copyright 2017 Citus Data Inc." under Apache-2.0, and is unchanged in
   this fork.
4. **Author lineage.** The imported history is authored by the upstream
   wal-g/wal-g maintainer team: Andrey Borodin, Daniil Zakhlystov, Philip
   Dubé, Dmitry Smal, Katie Li, and others. Commits carry upstream pull
   request numbers (#2269, #2312, #234, ...) matching that project.
5. **Fork integration.** This repository's git remote `original` points to
   `lateos-ai/wal-g-original` (the fork author's own mirror of upstream);
   its `master` tip is the fork point commit above. The integration merge
   `4b521c63` (2026-06-06) takes that commit as its second parent.
6. **Exhaustive reference search.** `grep -rn "golang-migrate"` across the
   whole repository (including docs/, mkdocs.yml, .github/, Dockerfiles,
   Helm charts) matches only the five lines in NOTICE and README.md that
   this correction edits.

## Disclaimer

The fork point was established from repository-internal evidence only. The
upstream release tag at the fork date could not be determined because no
upstream tags are present in this repository; this document deliberately
makes no release-version claim. A truthful provenance record would require
the upstream tag to be located externally (e.g. by matching the fork point
SHA in the upstream repository).
