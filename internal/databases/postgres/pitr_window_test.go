// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/memory"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

var pitrEpoch = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

// segmentSet builds the segments in the given ranges on a timeline, together
// with upload times one minute apart so window ends are distinguishable.
func segmentSet(timeline uint32, ranges ...[2]uint64) (map[WalSegmentDescription]bool,
	map[WalSegmentDescription]time.Time) {
	segments := make(map[WalSegmentDescription]bool)
	times := make(map[WalSegmentDescription]time.Time)

	for _, r := range ranges {
		for number := r[0]; number <= r[1]; number++ {
			description := WalSegmentDescription{Timeline: timeline, Number: WalSegmentNo(number)}
			segments[description] = true
			times[description] = pitrEpoch.Add(time.Duration(number) * time.Minute)
		}
	}

	return segments, times
}

// backupAt builds a backup that starts and finishes within the given segments.
func backupAt(name string, timeline uint32, start, finish uint64) PITRBackup {
	return PITRBackup{
		Name:          name,
		Timeline:      timeline,
		StartTime:     pitrEpoch.Add(time.Duration(start) * time.Minute),
		FinishTime:    pitrEpoch.Add(time.Duration(finish) * time.Minute),
		StartSegment:  WalSegmentNo(start),
		FinishSegment: WalSegmentNo(finish),
	}
}

func TestPITRWindow_SingleBackupWithContiguousWAL(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 5})
	backup := backupAt("base_a", 1, 1, 2)

	report := ComputePITRWindow([]PITRBackup{backup}, segments, times)

	if len(report.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d: %+v", len(report.Windows), report.Windows)
	}
	if report.RestorableBackups != 1 {
		t.Errorf("expected 1 restorable backup, got %d", report.RestorableBackups)
	}

	window := report.Windows[0]
	if !window.Start.Equal(backup.FinishTime) {
		t.Errorf("window should open when the backup finishes: got %s, want %s",
			window.Start, backup.FinishTime)
	}
	// The run continues to segment 5, so that is where recovery can reach.
	if want := times[WalSegmentDescription{Timeline: 1, Number: 5}]; !window.End.Equal(want) {
		t.Errorf("window should end at the last contiguous segment: got %s, want %s", window.End, want)
	}
}

// A backup missing the WAL written while it ran cannot reach a consistent state,
// so it restores to nothing at all rather than to a shorter window.
func TestPITRWindow_BackupMissingItsOwnWALIsUnrestorable(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 1}, [2]uint64{3, 5})
	backup := backupAt("base_a", 1, 1, 3)

	report := ComputePITRWindow([]PITRBackup{backup}, segments, times)

	if len(report.Windows) != 0 {
		t.Errorf("expected no windows, got %+v", report.Windows)
	}
	if len(report.Unrestorable) != 1 {
		t.Fatalf("expected 1 unrestorable backup, got %+v", report.Unrestorable)
	}
	if report.Unrestorable[0].Reason != PITRMissingOwnWAL {
		t.Errorf("expected reason %q, got %q", PITRMissingOwnWAL, report.Unrestorable[0].Reason)
	}
	if !report.Empty() {
		t.Error("a report with no windows should be empty")
	}
}

func TestPITRWindow_HoleInWALEndsTheWindow(t *testing.T) {
	// Segment 4 is missing, so recovery cannot pass it even though 5 and 6 exist.
	segments, times := segmentSet(1, [2]uint64{1, 3}, [2]uint64{5, 6})
	backup := backupAt("base_a", 1, 1, 1)

	report := ComputePITRWindow([]PITRBackup{backup}, segments, times)

	if len(report.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(report.Windows))
	}
	if want := times[WalSegmentDescription{Timeline: 1, Number: 3}]; !report.Windows[0].End.Equal(want) {
		t.Errorf("window should stop at the hole: got %s, want %s", report.Windows[0].End, want)
	}
}

func TestPITRWindow_SeparatedBackupsProduceAGap(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 3}, [2]uint64{10, 12})

	report := ComputePITRWindow([]PITRBackup{
		backupAt("base_a", 1, 1, 1),
		backupAt("base_b", 1, 10, 10),
	}, segments, times)

	if len(report.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d: %+v", len(report.Windows), report.Windows)
	}
	if len(report.Gaps) != 1 {
		t.Fatalf("expected 1 gap, got %+v", report.Gaps)
	}

	gap := report.Gaps[0]
	if !gap.Start.Equal(report.Windows[0].End) || !gap.End.Equal(report.Windows[1].Start) {
		t.Errorf("gap should span between the windows: %+v", gap)
	}

	// Coverage counts only recoverable time, so it must be less than the span
	// between the earliest and latest restorable points.
	span := report.LatestRestorableTime.Sub(report.EarliestRestorableTime)
	if report.CoverageDuration() >= span {
		t.Errorf("coverage %s should be less than the span %s", report.CoverageDuration(), span)
	}
}

// Every backup in one unbroken run of WAL reaches the same end, so reporting one
// window per backup would bury the single range an operator can actually use.
func TestPITRWindow_OverlappingWindowsMerge(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})

	report := ComputePITRWindow([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 8, 9),
		backupAt("base_c", 1, 15, 16),
	}, segments, times)

	if len(report.Windows) != 1 {
		t.Fatalf("expected the windows to merge into 1, got %d: %+v", len(report.Windows), report.Windows)
	}
	if len(report.Windows[0].Backups) != 3 {
		t.Errorf("merged window should list all 3 backups, got %v", report.Windows[0].Backups)
	}
	if len(report.Gaps) != 0 {
		t.Errorf("expected no gaps, got %+v", report.Gaps)
	}
}

func TestPITRWindow_DeltaWithMissingBaseIsUnrestorable(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})

	delta := backupAt("base_b_D_base_a", 1, 5, 6)
	delta.IncrementFrom = "base_a"
	delta.IncrementFull = "base_a"

	report := ComputePITRWindow([]PITRBackup{delta}, segments, times)

	if len(report.Unrestorable) != 1 {
		t.Fatalf("expected the delta to be unrestorable, got %+v", report.Unrestorable)
	}
	if report.Unrestorable[0].Reason != PITRBrokenChain {
		t.Errorf("expected reason %q, got %q", PITRBrokenChain, report.Unrestorable[0].Reason)
	}
	if len(report.Windows) != 0 {
		t.Errorf("a delta with no base opens no window, got %+v", report.Windows)
	}
}

func TestPITRWindow_DeltaWithPresentBaseIsRestorable(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})

	base := backupAt("base_a", 1, 1, 2)
	delta := backupAt("base_b_D_base_a", 1, 5, 6)
	delta.IncrementFrom = "base_a"
	delta.IncrementFull = "base_a"

	report := ComputePITRWindow([]PITRBackup{base, delta}, segments, times)

	if len(report.Unrestorable) != 0 {
		t.Fatalf("expected nothing unrestorable, got %+v", report.Unrestorable)
	}
	if report.RestorableBackups != 2 {
		t.Errorf("expected 2 restorable backups, got %d", report.RestorableBackups)
	}
}

// A chain that points at itself must not loop the report forever.
func TestPITRWindow_CyclicChainIsUnrestorableRatherThanFatal(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})

	first := backupAt("base_a", 1, 1, 2)
	first.IncrementFrom = "base_b"
	second := backupAt("base_b", 1, 3, 4)
	second.IncrementFrom = "base_a"

	report := ComputePITRWindow([]PITRBackup{first, second}, segments, times)

	if len(report.Unrestorable) != 2 {
		t.Fatalf("expected both backups to be unrestorable, got %+v", report.Unrestorable)
	}
}

// Clock skew or a reuploaded segment must not produce a window that ends before
// it starts, which would read as corruption rather than as noisy timestamps.
func TestPITRWindow_EndNeverPrecedesStart(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 3})

	backup := backupAt("base_a", 1, 1, 3)
	backup.FinishTime = pitrEpoch.Add(time.Hour)

	report := ComputePITRWindow([]PITRBackup{backup}, segments, times)

	if len(report.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(report.Windows))
	}
	if report.Windows[0].End.Before(report.Windows[0].Start) {
		t.Errorf("window ends before it starts: %+v", report.Windows[0])
	}
}

func TestPITRWindow_NoBackupsMeansNothingRestorable(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 100})

	report := ComputePITRWindow(nil, segments, times)

	if !report.Empty() {
		t.Errorf("WAL without a backup restores nothing, got %+v", report.Windows)
	}
}

// putPITRBackup writes the sentinel and metadata that LoadPITRInputs reads. The
// backup name carries the start WAL segment, which is where the timeline comes
// from, so it must agree with startLSN.
func putPITRBackup(t *testing.T, root storage.Folder, name string, startLSN, finishLSN LSN,
	start, finish time.Time) {
	t.Helper()

	sentinel := BackupSentinelDto{
		BackupStartLSN:  &startLSN,
		BackupFinishLSN: &finishLSN,
		PgVersion:       15,
	}

	if err := root.PutObject(utility.BaseBackupPath+internal.SentinelNameFromBackup(name),
		bytes.NewReader(mustMarshal(t, sentinel))); err != nil {
		t.Fatalf("failed to write sentinel: %v", err)
	}

	if err := root.PutObject(utility.BaseBackupPath+name+"/"+utility.MetadataFileName,
		bytes.NewReader(mustMarshal(t, ExtendedMetadataDto{
			StartTime:  start,
			FinishTime: finish,
			StartLsn:   startLSN,
			FinishLsn:  finishLSN,
		}))); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}
}

func putPITRSegments(t *testing.T, root storage.Folder, timeline uint32, from, to uint64) {
	t.Helper()

	for number := from; number <= to; number++ {
		name := utility.WalPath + WalSegmentNo(number).GetFilename(timeline) + ".lz4"
		if err := root.PutObject(name, bytes.NewReader([]byte("wal"))); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
}

// An end-to-end pass over real storage objects, covering the listing and
// sentinel reading that the pure-function tests deliberately skip.
func TestHandlePITRWindow_ReportsARestorableStorage(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	// base_000000010000000000000002 starts at segment 2 on timeline 1.
	start := LSN(2 * WalSegmentSize)
	finish := LSN(3*WalSegmentSize) - 1
	putPITRBackup(t, root, "base_000000010000000000000002", start, finish,
		pitrEpoch, pitrEpoch.Add(time.Minute))
	putPITRSegments(t, root, 1, 2, 6)

	buf := &bytes.Buffer{}
	code := HandlePITRWindow(root, PITRWindowOptions{Format: "json"}, buf)

	if code != 0 {
		t.Errorf("expected exit code 0 for a restorable storage, got %d\n%s", code, buf.String())
	}

	var report PITRWindowReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("pitr-window JSON does not parse: %v\noutput: %s", err, buf.String())
	}

	if len(report.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d: %s", len(report.Windows), buf.String())
	}
	if report.RestorableBackups != 1 || report.TotalBackups != 1 {
		t.Errorf("expected 1 of 1 backups restorable, got %d of %d",
			report.RestorableBackups, report.TotalBackups)
	}
	if report.Windows[0].Timeline != 1 {
		t.Errorf("expected timeline 1, got %d", report.Windows[0].Timeline)
	}
}

// Nothing restorable is a condition worth alerting on, so it must not exit 0.
func TestHandlePITRWindow_EmptyStorageExitsNonZero(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	buf := &bytes.Buffer{}
	if code := HandlePITRWindow(root, PITRWindowOptions{Format: "json"}, buf); code != 1 {
		t.Errorf("expected exit code 1 for an empty storage, got %d", code)
	}
}

func TestHandlePITRWindow_MinWindowGate(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	start := LSN(2 * WalSegmentSize)
	finish := LSN(3*WalSegmentSize) - 1
	// The window runs from the backup's finish time to the last WAL segment,
	// and the segments are written now. Anchoring the backup to a fixed date
	// made the span widen with the calendar until it cleared any threshold, so
	// it has to be as recent as the segments are.
	recent := time.Now().Add(-time.Minute)
	putPITRBackup(t, root, "base_000000010000000000000002", start, finish,
		recent, recent.Add(time.Second))
	putPITRSegments(t, root, 1, 2, 6)

	buf := &bytes.Buffer{}
	// The recoverable span is now seconds wide; a week-long requirement
	// cannot be met.
	if code := HandlePITRWindow(root, PITRWindowOptions{
		Format:    "json",
		MinWindow: 7 * 24 * time.Hour,
	}, buf); code != 1 {
		t.Errorf("expected exit code 1 when the window is shorter than --min-window, got %d", code)
	}
}

func TestClassifyStorageNames(t *testing.T) {
	backups, segments := ClassifyStorageNames([]string{
		"basebackups_005/base_000000010000000000000002/tar_partitions/part_001.tar.lz4",
		"basebackups_005/base_000000010000000000000002_backup_stop_sentinel.json",
		"basebackups_005/base_000000010000000000000009_D_000000010000000000000002/metadata.json",
		"wal_005/000000010000000000000003.lz4",
		"wal_005/00000002.history",
		"some/unrelated/object",
	})

	if len(backups) != 2 {
		t.Errorf("expected 2 backups, got %v", backups)
	}
	if !backups["base_000000010000000000000002"] {
		t.Errorf("sentinel and partition should map to the same backup, got %v", backups)
	}
	if !backups["base_000000010000000000000009_D_000000010000000000000002"] {
		t.Errorf("delta backup name should survive classification, got %v", backups)
	}
	if len(segments) != 1 {
		t.Errorf("history files are not segments, got %v", segments)
	}
}

func TestPITRInputs_WithoutDropsBackupsAndSegments(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})
	inputs := PITRInputs{
		Backups:      []PITRBackup{backupAt("base_a", 1, 1, 2), backupAt("base_b", 1, 5, 6)},
		Segments:     segments,
		SegmentTimes: times,
	}

	remaining := inputs.Without([]string{
		"basebackups_005/base_a/metadata.json",
		"wal_005/" + WalSegmentNo(3).GetFilename(1) + ".lz4",
	})

	if len(remaining.Backups) != 1 || remaining.Backups[0].Name != "base_b" {
		t.Errorf("expected only base_b to remain, got %+v", remaining.Backups)
	}
	if remaining.Segments[WalSegmentDescription{Timeline: 1, Number: 3}] {
		t.Error("deleted segment should not remain")
	}
	if len(remaining.Segments) != len(segments)-1 {
		t.Errorf("expected exactly one segment removed, got %d of %d",
			len(remaining.Segments), len(segments))
	}

	// The originals must be untouched: --explain computes the before and after
	// windows from the same snapshot.
	if len(inputs.Backups) != 2 || len(inputs.Segments) != 10 {
		t.Error("Without must not mutate the inputs it was called on")
	}
}

// Deleting one file of a backup does not leave a smaller backup, it leaves an
// unusable one.
func TestPITRInputs_WithoutDropsBackupOnAnySingleObject(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})
	inputs := PITRInputs{
		Backups:      []PITRBackup{backupAt("base_a", 1, 1, 2)},
		Segments:     segments,
		SegmentTimes: times,
	}

	remaining := inputs.Without([]string{
		"basebackups_005/base_a/tar_partitions/part_003.tar.lz4",
	})

	if len(remaining.Backups) != 0 {
		t.Errorf("expected the backup to be dropped, got %+v", remaining.Backups)
	}
}
