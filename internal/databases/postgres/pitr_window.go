// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

// PITRBlockReason says why a backup cannot serve a point-in-time restore.
type PITRBlockReason string

const (
	// PITRMissingOwnWAL means WAL written between the backup's start and finish
	// LSN is absent, so the backup cannot be replayed to a consistent state. Such
	// a backup restores to nothing at all, not merely to a shorter window.
	PITRMissingOwnWAL PITRBlockReason = "missing_own_wal"
	// PITRBrokenChain means the backup is a delta whose base, or some link on the
	// way to it, is not in storage.
	PITRBrokenChain PITRBlockReason = "broken_increment_chain"
)

// PITRBackup is what the window computation needs to know about one backup. It
// is deliberately a plain value: the computation is a pure function of the
// backups and WAL present, which is what lets delete --explain run it twice,
// once against storage as it is and once against storage as it would be.
type PITRBackup struct {
	Name        string
	Timeline    uint32
	StartTime   time.Time
	FinishTime  time.Time
	IsPermanent bool

	// StartSegment and FinishSegment bound the WAL a restore must replay to
	// bring this backup to a consistent state.
	StartSegment  WalSegmentNo
	FinishSegment WalSegmentNo

	// IncrementFrom is the backup this one is a delta of, empty for a full
	// backup. IncrementFull is the full backup at the base of the chain.
	IncrementFrom string
	IncrementFull string
}

// PITRWindow is one continuous range of time a cluster can be recovered to.
type PITRWindow struct {
	Timeline uint32 `json:"timeline"`
	// Start is the earliest recoverable point in this window: the finish time of
	// the oldest backup that reaches consistency within it. Recovery cannot
	// target a moment part-way through a backup, so the window opens when the
	// backup closes, not when it started.
	Start time.Time `json:"start"`
	// End is the last recoverable point, taken from the upload time of the last
	// unbroken WAL segment after the backup.
	End          time.Time `json:"end"`
	StartSegment string    `json:"start_segment"`
	EndSegment   string    `json:"end_segment"`
	// Backups are the backups that can serve as a base for a restore into this
	// window, oldest first.
	Backups []string `json:"backups"`
}

// Duration is how much recoverable time the window covers.
func (w PITRWindow) Duration() time.Duration {
	return w.End.Sub(w.Start)
}

// PITRGap is a stretch between two windows on one timeline that no backup and
// WAL combination can recover to.
type PITRGap struct {
	Timeline uint32    `json:"timeline"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
}

// Duration is how much unrecoverable time the gap covers.
func (g PITRGap) Duration() time.Duration {
	return g.End.Sub(g.Start)
}

// PITRUnrestorableBackup is a backup that is present but cannot be restored.
type PITRUnrestorableBackup struct {
	Name   string          `json:"name"`
	Reason PITRBlockReason `json:"reason"`
	Detail string          `json:"detail"`
}

// PITRWindowReport is the recoverable state of a storage: which moments can be
// restored to, which cannot, and which backups are dead weight.
type PITRWindowReport struct {
	Windows      []PITRWindow             `json:"windows"`
	Gaps         []PITRGap                `json:"gaps,omitempty"`
	Unrestorable []PITRUnrestorableBackup `json:"unrestorable_backups,omitempty"`

	// EarliestRestorableTime and LatestRestorableTime bound every window. The
	// span between them is not necessarily recoverable in full - see Gaps.
	EarliestRestorableTime time.Time `json:"earliest_restorable_time,omitempty"`
	LatestRestorableTime   time.Time `json:"latest_restorable_time,omitempty"`

	// Coverage is the recoverable time summed across windows, which is less than
	// the span between earliest and latest when there are gaps.
	Coverage string `json:"coverage,omitempty"`

	TotalBackups      int `json:"total_backups"`
	RestorableBackups int `json:"restorable_backups"`
}

// Empty reports whether nothing at all can be restored.
func (r PITRWindowReport) Empty() bool {
	return len(r.Windows) == 0
}

// CoverageDuration is Coverage as a duration rather than as text.
func (r PITRWindowReport) CoverageDuration() time.Duration {
	var total time.Duration
	for _, window := range r.Windows {
		total += window.Duration()
	}
	return total
}

// ComputePITRWindow derives the ranges of time a cluster can be recovered to
// from the backups and WAL segments present.
//
// A restore needs two things: a backup that can reach consistency, and an
// unbroken run of WAL from that backup forward to the target moment. So each
// backup opens a window at its finish time that runs until the first missing
// WAL segment after it. Overlapping windows are merged, and what is left
// between them is a gap - a stretch of history that is simply not recoverable,
// however many backups sit on either side of it.
//
// Window ends come from the upload time of the last WAL segment in the run,
// which approximates the wall-clock time of the transactions inside it. WAL
// records carry commit timestamps, but reading them would mean fetching and
// decrypting every segment; the upload time is close enough to size a recovery
// window and costs one listing.
//
// Windows are computed per timeline. A restore that follows a timeline switch
// is possible, but which fork a given moment belongs to depends on the recovery
// target, so folding timelines together would report windows that no single
// restore could actually deliver.
func ComputePITRWindow(
	backups []PITRBackup,
	segments map[WalSegmentDescription]bool,
	segmentTimes map[WalSegmentDescription]time.Time,
) PITRWindowReport {
	report := PITRWindowReport{TotalBackups: len(backups)}

	byName := make(map[string]PITRBackup, len(backups))
	for i := range backups {
		byName[backups[i].Name] = backups[i]
	}

	// Sort by finish time so that windows, and the backups listed against them,
	// come out oldest first regardless of the order storage listed them in.
	ordered := make([]PITRBackup, len(backups))
	copy(ordered, backups)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].FinishTime.Equal(ordered[j].FinishTime) {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].FinishTime.Before(ordered[j].FinishTime)
	})

	unmerged := make([]PITRWindow, 0, len(ordered))

	for i := range ordered {
		backup := &ordered[i]

		if missing, broken := brokenChainLink(backup, byName); broken {
			report.Unrestorable = append(report.Unrestorable, PITRUnrestorableBackup{
				Name:   backup.Name,
				Reason: PITRBrokenChain,
				Detail: fmt.Sprintf("delta backup needs %s, which is not in storage", missing),
			})
			continue
		}

		if missing, ok := firstMissingSegment(segments, backup.Timeline,
			backup.StartSegment, backup.FinishSegment); !ok {
			report.Unrestorable = append(report.Unrestorable, PITRUnrestorableBackup{
				Name:   backup.Name,
				Reason: PITRMissingOwnWAL,
				Detail: fmt.Sprintf("WAL segment %s is missing, so the backup cannot reach a consistent state",
					missing.GetFileName()),
			})
			continue
		}

		report.RestorableBackups++

		last := lastContiguousSegment(segments, backup.Timeline, backup.FinishSegment)
		end := segmentEndTime(segmentTimes, backup.Timeline, last, backup.FinishTime)

		unmerged = append(unmerged, PITRWindow{
			Timeline:     backup.Timeline,
			Start:        backup.FinishTime,
			End:          end,
			StartSegment: backup.FinishSegment.GetFilename(backup.Timeline),
			EndSegment:   last.GetFilename(backup.Timeline),
			Backups:      []string{backup.Name},
		})
	}

	report.Windows = mergeWindows(unmerged)
	report.Gaps = findGaps(report.Windows)

	for i, window := range report.Windows {
		if i == 0 || window.Start.Before(report.EarliestRestorableTime) {
			report.EarliestRestorableTime = window.Start
		}
		if i == 0 || window.End.After(report.LatestRestorableTime) {
			report.LatestRestorableTime = window.End
		}
	}

	if !report.Empty() {
		report.Coverage = report.CoverageDuration().Round(time.Second).String()
	}

	return report
}

// brokenChainLink walks a delta backup's chain to its base and returns the
// first link that is absent. A delta whose base is gone restores to nothing,
// which is worth saying plainly: it still occupies storage and still appears in
// backup-list.
func brokenChainLink(backup *PITRBackup, byName map[string]PITRBackup) (string, bool) {
	seen := map[string]bool{backup.Name: true}
	current := *backup

	for current.IncrementFrom != "" {
		parent, ok := byName[current.IncrementFrom]
		if !ok {
			return current.IncrementFrom, true
		}
		if seen[parent.Name] {
			// A cycle cannot be restored either, and looping forever while
			// explaining a delete would be worse than saying so.
			return parent.Name, true
		}
		seen[parent.Name] = true
		current = parent
	}

	// A chain that terminates should terminate at the recorded full backup.
	if backup.IncrementFull != "" {
		if _, ok := byName[backup.IncrementFull]; !ok {
			return backup.IncrementFull, true
		}
	}

	return "", false
}

// firstMissingSegment reports the first absent segment in [from, to], and
// whether the whole range is present.
func firstMissingSegment(
	segments map[WalSegmentDescription]bool,
	timeline uint32,
	from, to WalSegmentNo,
) (WalSegmentDescription, bool) {
	for segment := from; segment <= to; segment = segment.Next() {
		description := WalSegmentDescription{Timeline: timeline, Number: segment}
		if !segments[description] {
			return description, false
		}
	}
	return WalSegmentDescription{}, true
}

// lastContiguousSegment walks forward from a segment while the next one is
// present, and returns where the run ends.
func lastContiguousSegment(
	segments map[WalSegmentDescription]bool,
	timeline uint32,
	from WalSegmentNo,
) WalSegmentNo {
	last := from
	for {
		next := last.Next()
		if !segments[WalSegmentDescription{Timeline: timeline, Number: next}] {
			return last
		}
		last = next
	}
}

// segmentEndTime dates the close of a window. A window can never end before it
// opens: a segment uploaded before the backup that needs it would otherwise
// produce a negative duration, which reads as corruption rather than as the
// clock skew or reupload it actually is.
func segmentEndTime(
	segmentTimes map[WalSegmentDescription]time.Time,
	timeline uint32,
	segment WalSegmentNo,
	notBefore time.Time,
) time.Time {
	end, ok := segmentTimes[WalSegmentDescription{Timeline: timeline, Number: segment}]
	if !ok || end.Before(notBefore) {
		return notBefore
	}
	return end
}

// mergeWindows folds overlapping and touching windows on the same timeline into
// one. Every backup in a contiguous run of WAL produces a window ending at the
// same place, so without merging a storage with fifty backups would report
// fifty nested windows instead of the one range an operator can actually use.
func mergeWindows(windows []PITRWindow) []PITRWindow {
	if len(windows) == 0 {
		return nil
	}

	sort.Slice(windows, func(i, j int) bool {
		if windows[i].Timeline != windows[j].Timeline {
			return windows[i].Timeline < windows[j].Timeline
		}
		return windows[i].Start.Before(windows[j].Start)
	})

	merged := []PITRWindow{windows[0]}

	for _, window := range windows[1:] {
		last := &merged[len(merged)-1]

		if window.Timeline != last.Timeline || window.Start.After(last.End) {
			merged = append(merged, window)
			continue
		}

		last.Backups = append(last.Backups, window.Backups...)
		if window.End.After(last.End) {
			last.End = window.End
			last.EndSegment = window.EndSegment
		}
	}

	return merged
}

// findGaps returns the stretches between consecutive windows on a timeline.
func findGaps(windows []PITRWindow) []PITRGap {
	var gaps []PITRGap

	for i := 1; i < len(windows); i++ {
		previous, current := windows[i-1], windows[i]
		if previous.Timeline != current.Timeline {
			continue
		}
		gaps = append(gaps, PITRGap{
			Timeline: current.Timeline,
			Start:    previous.End,
			End:      current.Start,
		})
	}

	return gaps
}

// PITRInputs is the storage state a window computation runs on.
type PITRInputs struct {
	Backups      []PITRBackup
	Segments     map[WalSegmentDescription]bool
	SegmentTimes map[WalSegmentDescription]time.Time
}

// Compute derives the recovery window from these inputs.
func (in PITRInputs) Compute() PITRWindowReport {
	return ComputePITRWindow(in.Backups, in.Segments, in.SegmentTimes)
}

// Without returns the inputs as they would be if the named storage objects were
// deleted, so the same computation can describe a storage that does not exist
// yet. Names are relative to the storage root.
//
// A backup is dropped when any of its objects is deleted: a backup missing one
// file is not a smaller backup, it is an unusable one.
func (in PITRInputs) Without(deletedNames []string) PITRInputs {
	deletedBackups, deletedSegments := ClassifyStorageNames(deletedNames)

	remaining := PITRInputs{
		Backups:      make([]PITRBackup, 0, len(in.Backups)),
		Segments:     make(map[WalSegmentDescription]bool, len(in.Segments)),
		SegmentTimes: make(map[WalSegmentDescription]time.Time, len(in.SegmentTimes)),
	}

	for i := range in.Backups {
		if !deletedBackups[in.Backups[i].Name] {
			remaining.Backups = append(remaining.Backups, in.Backups[i])
		}
	}

	for segment := range in.Segments {
		if !deletedSegments[segment] {
			remaining.Segments[segment] = true
			if uploaded, ok := in.SegmentTimes[segment]; ok {
				remaining.SegmentTimes[segment] = uploaded
			}
		}
	}

	return remaining
}

// PITRWindowOptions carries the tunables for a pitr-window run.
type PITRWindowOptions struct {
	// Format is "text" or "json".
	Format string
	// MinWindow, when set, is the recoverable span the storage is expected to
	// hold. Falling short of it exits non-zero so a retention policy that has
	// quietly stopped covering its stated RPO can be caught by a CI gate.
	MinWindow time.Duration
}

// HandlePITRWindow reports what the storage can currently be recovered to. It
// returns the process exit code: 0 when something is restorable and any
// requested minimum is met, 1 otherwise.
func HandlePITRWindow(rootFolder storage.Folder, opts PITRWindowOptions, output io.Writer) int {
	inputs, err := LoadPITRInputs(rootFolder)
	if err != nil {
		tracelog.ErrorLogger.Printf("Failed to read storage: %v\n", err)
		return 1
	}

	report := inputs.Compute()

	if err := WritePITRWindowReport(&report, opts.Format, output); err != nil {
		tracelog.ErrorLogger.Printf("Failed to write the report: %v\n", err)
		return 1
	}

	if report.Empty() {
		return 1
	}

	if opts.MinWindow > 0 && report.CoverageDuration() < opts.MinWindow {
		return 1
	}

	return 0
}

// WritePITRWindowReport renders a window report.
func WritePITRWindowReport(report *PITRWindowReport, format string, output io.Writer) error {
	if format == "json" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, err = output.Write(append(data, '\n'))
		return err
	}

	writePITRWindowText(report, output)

	return nil
}

func writePITRWindowText(report *PITRWindowReport, output io.Writer) {
	fmt.Fprintf(output, "wal-g pitr-window\n\n")

	if report.Empty() {
		fmt.Fprintf(output, "Nothing is restorable.\n\n")
	} else {
		fmt.Fprintf(output, "Recoverable  %s .. %s  (%s across %d window(s))\n\n",
			formatPITRTime(report.EarliestRestorableTime),
			formatPITRTime(report.LatestRestorableTime),
			formatDuration(report.CoverageDuration()),
			len(report.Windows))

		for _, window := range report.Windows {
			fmt.Fprintf(output, "  timeline %d  %s .. %s  (%s)\n",
				window.Timeline,
				formatPITRTime(window.Start),
				formatPITRTime(window.End),
				formatDuration(window.Duration()))
			fmt.Fprintf(output, "              WAL %s .. %s, %d backup(s)\n",
				window.StartSegment, window.EndSegment, len(window.Backups))
		}

		fmt.Fprintln(output)
	}

	if len(report.Gaps) > 0 {
		fmt.Fprintf(output, "Gaps - these ranges cannot be recovered to\n")
		for _, gap := range report.Gaps {
			fmt.Fprintf(output, "  timeline %d  %s .. %s  (%s)\n",
				gap.Timeline, formatPITRTime(gap.Start), formatPITRTime(gap.End),
				formatDuration(gap.Duration()))
		}
		fmt.Fprintln(output)
	}

	if len(report.Unrestorable) > 0 {
		fmt.Fprintf(output, "Backups that cannot serve a restore\n")
		for _, unrestorable := range report.Unrestorable {
			fmt.Fprintf(output, "  %s: %s\n", unrestorable.Name, unrestorable.Detail)
		}
		fmt.Fprintln(output)
	}

	fmt.Fprintf(output, "%d of %d backup(s) can serve a restore.\n",
		report.RestorableBackups, report.TotalBackups)
}

func formatPITRTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// ClassifyStorageNames sorts root-relative object names into the backups and the
// WAL segments they belong to. Objects that are neither - .history files, the
// leftovers "delete garbage" targets - belong to no backup and no segment, and
// are simply not returned.
func ClassifyStorageNames(names []string) (map[string]bool, map[WalSegmentDescription]bool) {
	backups := make(map[string]bool)
	segments := make(map[WalSegmentDescription]bool)

	for _, name := range names {
		switch {
		case strings.HasPrefix(name, utility.BaseBackupPath):
			relative := strings.TrimPrefix(name, utility.BaseBackupPath)
			if backupName := utility.StripLeftmostBackupName(relative); backupName != "" {
				backups[backupName] = true
			}

		case strings.HasPrefix(name, utility.WalPath):
			relative := strings.TrimPrefix(name, utility.WalPath)
			segment, err := NewWalSegmentDescription(utility.TrimFileExtension(relative))
			if err != nil {
				continue
			}
			segments[segment] = true
		}
	}

	return backups, segments
}

// LoadPITRInputs reads the backups and WAL segments in storage into the form
// the window computation runs on.
func LoadPITRInputs(rootFolder storage.Folder) (PITRInputs, error) {
	inputs := PITRInputs{
		Segments:     make(map[WalSegmentDescription]bool),
		SegmentTimes: make(map[WalSegmentDescription]time.Time),
	}

	walFolder := rootFolder.GetSubFolder(utility.WalPath)
	walObjects, _, err := walFolder.ListFolder()
	if err != nil {
		return inputs, fmt.Errorf("failed to list the WAL folder: %w", err)
	}

	for _, object := range walObjects {
		segment, err := NewWalSegmentDescription(utility.TrimFileExtension(object.GetName()))
		if err != nil {
			continue
		}
		inputs.Segments[segment] = true
		// A segment can be uploaded more than once. The latest upload is the one
		// that is actually readable now.
		if existing, ok := inputs.SegmentTimes[segment]; !ok || object.GetLastModified().After(existing) {
			inputs.SegmentTimes[segment] = object.GetLastModified()
		}
	}

	inputs.Backups, err = loadPITRBackups(rootFolder)
	if err != nil {
		return inputs, err
	}

	return inputs, nil
}

// loadPITRBackups reads each backup's metadata and sentinel. Backups whose
// metadata cannot be read are skipped with a warning rather than failing the
// whole report: one unreadable sentinel should not stop an operator from
// finding out what the rest of the storage can recover to.
func loadPITRBackups(rootFolder storage.Folder) ([]PITRBackup, error) {
	baseBackupFolder := rootFolder.GetSubFolder(utility.BaseBackupPath)

	backupTimes, err := internal.GetBackups(baseBackupFolder)
	if err != nil {
		if _, ok := err.(internal.NoBackupsFoundError); ok {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	backups := make([]PITRBackup, 0, len(backupTimes))

	for _, backupTime := range backupTimes {
		detail, err := GetBackupDetails(baseBackupFolder, backupTime)
		if err != nil {
			tracelog.WarningLogger.Printf(
				"Skipping %s in the recovery window: failed to read its metadata: %v\n",
				backupTime.BackupName, err)
			continue
		}

		timeline, _, err := ParseWALFilename(detail.WalFileName)
		if err != nil {
			tracelog.WarningLogger.Printf(
				"Skipping %s in the recovery window: cannot parse its start WAL name %q: %v\n",
				backupTime.BackupName, detail.WalFileName, err)
			continue
		}

		backup := PITRBackup{
			Name:          detail.BackupName,
			Timeline:      timeline,
			StartTime:     detail.StartTime,
			FinishTime:    detail.FinishTime,
			IsPermanent:   detail.IsPermanent,
			StartSegment:  NewWalSegmentNo(detail.StartLsn),
			FinishSegment: NewWalSegmentNo(detail.FinishLsn),
		}

		if incrementFrom, incrementFull, ok := readIncrementChain(baseBackupFolder, backupTime); ok {
			backup.IncrementFrom = incrementFrom
			backup.IncrementFull = incrementFull
		}

		backups = append(backups, backup)
	}

	return backups, nil
}

// readIncrementChain reads a backup's delta lineage from its sentinel. A
// failure here is reported as "not a delta" only because the alternative -
// dropping the backup entirely - would understate what can be recovered.
func readIncrementChain(baseBackupFolder storage.Folder, backupTime internal.BackupTime) (string, string, bool) {
	backup, err := NewBackupInStorage(baseBackupFolder, backupTime.BackupName, backupTime.StorageName)
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to open %s: %v\n", backupTime.BackupName, err)
		return "", "", false
	}

	sentinel, err := backup.GetSentinel()
	if err != nil {
		tracelog.WarningLogger.Printf("Failed to read the sentinel of %s: %v\n", backupTime.BackupName, err)
		return "", "", false
	}

	if !sentinel.IsIncremental() {
		return "", "", true
	}

	return *sentinel.IncrementFrom, *sentinel.IncrementFullName, true
}
