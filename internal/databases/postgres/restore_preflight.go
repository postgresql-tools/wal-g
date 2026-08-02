// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal/fsutil"
)

// SpaceVerdict is the outcome of sizing a restore against the free space
// available for it.
type SpaceVerdict string

const (
	// SpaceSufficient means the restore fits, with the configured margin.
	SpaceSufficient SpaceVerdict = "sufficient"
	// SpaceInsufficient means the restore demonstrably does not fit. This is only
	// ever reported when it can be established without guessing.
	SpaceInsufficient SpaceVerdict = "insufficient"
	// SpaceIndeterminate means the restore spans filesystems whose individual
	// shares of the total are not recorded in the backup, so whether it fits
	// cannot be decided - only that the combined free space is large enough for
	// the total.
	SpaceIndeterminate SpaceVerdict = "indeterminate"
	// SpaceUnknown means the backup records no uncompressed size to size against.
	SpaceUnknown SpaceVerdict = "unknown"
)

// DefaultRestoreSpaceMargin is the multiple of a backup's uncompressed size that
// should be free before a restore is considered safe. The headroom covers WAL
// replayed during recovery and the cluster accepting writes once it is up.
const DefaultRestoreSpaceMargin = 1.2

// FilesystemSpace is the capacity of one filesystem a restore would write to,
// together with the restore paths that land on it.
type FilesystemSpace struct {
	ID         string   `json:"id"`
	Paths      []string `json:"paths"`
	FreeBytes  int64    `json:"free_bytes"`
	TotalBytes int64    `json:"total_bytes"`
}

// RestorePreflight is the result of sizing a restore before starting it.
type RestorePreflight struct {
	BackupName string       `json:"backup_name"`
	Verdict    SpaceVerdict `json:"verdict"`
	// RequiredBytes is the backup's uncompressed size.
	RequiredBytes int64 `json:"required_bytes"`
	// NeededBytes is RequiredBytes with the margin applied.
	NeededBytes int64   `json:"needed_bytes"`
	Margin      float64 `json:"margin"`
	// FreeBytes is the combined free space across every filesystem involved.
	FreeBytes   int64             `json:"free_bytes"`
	Filesystems []FilesystemSpace `json:"filesystems"`
	// Reason explains a verdict that is not simply "sufficient".
	Reason string `json:"reason,omitempty"`
}

// Fits reports whether the restore is known not to fit. Indeterminate and
// unknown verdicts are not failures: refusing on them would block restores this
// check cannot actually rule on.
func (p RestorePreflight) Fits() bool {
	return p.Verdict != SpaceInsufficient
}

// Summary renders the verdict as a single line for logs and reports.
func (p RestorePreflight) Summary() string {
	switch p.Verdict {
	case SpaceSufficient:
		return fmt.Sprintf("%s free, restore of %s needs ~%s",
			formatBytes(p.FreeBytes), p.BackupName, formatBytes(p.NeededBytes))
	case SpaceInsufficient:
		return fmt.Sprintf("not enough free space to restore %s: %s free, ~%s needed",
			p.BackupName, formatBytes(p.FreeBytes), formatBytes(p.NeededBytes))
	case SpaceIndeterminate:
		return fmt.Sprintf("cannot size the restore of %s precisely: %s",
			p.BackupName, p.Reason)
	default:
		return fmt.Sprintf("cannot size the restore of %s: %s", p.BackupName, p.Reason)
	}
}

// CheckRestoreSpace sizes a restore of backup into dataDir against the free
// space actually available to it.
//
// The backup's sentinel records only a total uncompressed size, with no
// per-file or per-tablespace breakdown. So when tablespaces put parts of the
// restore on different filesystems, how the total divides between them is not
// knowable here. Rather than guess - and either block a restore that would have
// fit or wave through one that would not - that case is reported as
// indeterminate, and only a total that cannot fit anywhere is called a failure.
func CheckRestoreSpace(backup Backup, dataDir string, margin float64) (RestorePreflight, error) {
	if margin <= 0 {
		margin = DefaultRestoreSpaceMargin
	}

	preflight := RestorePreflight{
		BackupName: backup.Name,
		Margin:     margin,
	}

	sentinel, err := backup.GetSentinel()
	if err != nil {
		return preflight, fmt.Errorf("failed to read the sentinel of %s: %w", backup.Name, err)
	}

	preflight.RequiredBytes = sentinel.UncompressedSize
	if preflight.RequiredBytes <= 0 {
		preflight.Verdict = SpaceUnknown
		preflight.Reason = "the backup does not record an uncompressed size"
		return preflight, nil
	}

	preflight.NeededBytes = int64(float64(preflight.RequiredBytes) * margin)

	paths := restoreTargetPaths(dataDir, sentinel.TablespaceSpec)

	filesystems, err := groupPathsByFilesystem(paths)
	if err != nil {
		return preflight, err
	}
	if len(filesystems) == 0 {
		preflight.Verdict = SpaceUnknown
		preflight.Reason = "none of the restore paths exist yet, so free space cannot be measured"
		return preflight, nil
	}

	preflight.Filesystems = filesystems
	for _, fs := range filesystems {
		preflight.FreeBytes += fs.FreeBytes
	}

	switch {
	case len(filesystems) == 1:
		// Everything lands on one filesystem, so the total is the whole story.
		if preflight.FreeBytes < preflight.NeededBytes {
			preflight.Verdict = SpaceInsufficient
		} else {
			preflight.Verdict = SpaceSufficient
		}

	case preflight.FreeBytes < preflight.NeededBytes:
		// Even pooling every filesystem the restore touches, the total does not
		// fit. No breakdown could rescue this.
		preflight.Verdict = SpaceInsufficient
		preflight.Reason = fmt.Sprintf(
			"the restore spans %d filesystems whose combined free space is still too small",
			len(filesystems))

	default:
		preflight.Verdict = SpaceIndeterminate
		preflight.Reason = fmt.Sprintf(
			"the restore spans %d filesystems and the backup records no per-tablespace sizes, "+
				"so the total fits overall but its distribution is unknown",
			len(filesystems))
	}

	return preflight, nil
}

// SpaceGuardOptions controls the space preflight that runs before a restore
// writes anything.
type SpaceGuardOptions struct {
	// Margin is the multiple of the backup's uncompressed size that must be free.
	Margin float64
	// Skip disables the preflight entirely.
	Skip bool
	// Force downgrades a refusal to a warning, for operators who know something
	// the check does not - a filesystem about to be grown, say.
	Force bool
}

// GuardRestoreSpace sizes a restore before it begins and stops it when it
// demonstrably will not fit. Aborting here costs a storage round-trip; finding
// out during extraction costs however many hours the restore had already run,
// and leaves a half-written data directory behind.
//
// It aborts the process rather than returning an error because the fetch path it
// guards reports all its failures that way.
func GuardRestoreSpace(backup Backup, dataDir string, opts SpaceGuardOptions) {
	if opts.Skip {
		tracelog.DebugLogger.Println("Restore space preflight skipped")
		return
	}

	preflight, err := CheckRestoreSpace(backup, dataDir, opts.Margin)
	if err != nil {
		// An unmeasurable filesystem is not a reason to refuse a restore the
		// operator asked for; say so and continue.
		tracelog.WarningLogger.Printf("Could not size the restore before starting it: %v", err)
		return
	}

	switch preflight.Verdict {
	case SpaceInsufficient:
		if opts.Force {
			tracelog.WarningLogger.Printf("%s; continuing anyway because --force was passed",
				preflight.Summary())
			return
		}
		tracelog.ErrorLogger.Fatalf(
			"%s\nFree space on the target filesystem, restore to a larger volume, "+
				"or pass --force to proceed anyway.\n%s\n",
			preflight.Summary(), preflight.Describe())

	case SpaceIndeterminate:
		tracelog.WarningLogger.Printf("%s", preflight.Describe())

	case SpaceUnknown:
		tracelog.InfoLogger.Printf("Restore space not checked: %s", preflight.Reason)

	default:
		tracelog.InfoLogger.Printf("Restore space check passed: %s", preflight.Summary())
	}
}

// restoreTargetPaths lists the directories a restore writes into: the data
// directory, plus any tablespace locations recorded in the backup.
func restoreTargetPaths(dataDir string, spec *TablespaceSpec) []string {
	paths := []string{dataDir}

	if spec == nil || spec.empty() {
		return paths
	}

	for _, location := range spec.tablespaceLocations() {
		if location.Location != "" {
			paths = append(paths, location.Location)
		}
	}

	return paths
}

// groupPathsByFilesystem collapses paths onto the filesystems that back them, so
// two tablespaces on one volume are counted against one pool of free space
// rather than being credited with it twice.
//
// Paths that do not exist yet are skipped: a tablespace directory is often
// created by the restore itself, and a missing path contributes no measurable
// free space either way.
func groupPathsByFilesystem(paths []string) ([]FilesystemSpace, error) {
	byID := make(map[fsutil.FilesystemID]*FilesystemSpace)
	var order []fsutil.FilesystemID

	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}

		id, err := fsutil.GetFilesystemID(path)
		if err != nil {
			return nil, fmt.Errorf("failed to identify the filesystem holding %s: %w", path, err)
		}

		if existing, ok := byID[id]; ok {
			existing.Paths = append(existing.Paths, path)
			continue
		}

		space, err := fsutil.GetDiskSpace(path)
		if err != nil {
			return nil, fmt.Errorf("failed to measure free space at %s: %w", path, err)
		}

		byID[id] = &FilesystemSpace{
			ID:         string(id),
			Paths:      []string{path},
			FreeBytes:  int64(space.FreeBytes),
			TotalBytes: int64(space.TotalBytes),
		}
		order = append(order, id)
	}

	result := make([]FilesystemSpace, 0, len(order))
	for _, id := range order {
		fs := byID[id]
		sort.Strings(fs.Paths)
		result = append(result, *fs)
	}

	return result, nil
}

// Describe renders the per-filesystem breakdown for a human reading a report.
func (p RestorePreflight) Describe() string {
	if len(p.Filesystems) == 0 {
		return p.Summary()
	}

	var b strings.Builder
	b.WriteString(p.Summary())

	if len(p.Filesystems) == 1 {
		return b.String()
	}

	for _, fs := range p.Filesystems {
		fmt.Fprintf(&b, "\n  %s: %s free of %s",
			strings.Join(fs.Paths, ", "), formatBytes(fs.FreeBytes), formatBytes(fs.TotalBytes))
	}

	return b.String()
}
