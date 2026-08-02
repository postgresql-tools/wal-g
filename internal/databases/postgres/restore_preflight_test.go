// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lateos-ai/wal-g/pkg/storages/memory"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

// putSizedBackup writes a backup whose sentinel records uncompressedSize and the
// supplied tablespace locations.
func putSizedBackup(t *testing.T, root storage.Folder, name string,
	uncompressedSize int64, tablespaces map[string]string,
) Backup {
	t.Helper()

	sentinel := BackupSentinelDto{
		BackupStartLSN:   lsnPtr(0x1000000),
		BackupFinishLSN:  lsnPtr(0x2000000),
		PgVersion:        15,
		UncompressedSize: uncompressedSize,
	}

	if len(tablespaces) > 0 {
		spec := NewTablespaceSpec("/var/lib/postgresql/data")
		for oid, location := range tablespaces {
			spec.addTablespace(oid, location)
		}
		sentinel.TablespaceSpec = &spec
	}

	data, err := json.Marshal(sentinel)
	if err != nil {
		t.Fatalf("failed to marshal sentinel: %v", err)
	}
	putBackupSentinel(root, name, data)

	filesMeta := minimalFilesMeta()
	putFilesMeta(root, name, filesMeta)
	putTarParts(root, name, realTarParts(filesMeta))

	backup, err := NewBackup(root.GetSubFolder(utility.BaseBackupPath), name)
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	return backup
}

func TestCheckRestoreSpace_SufficientOnSingleFilesystem(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	backup := putSizedBackup(t, root, testBackupName, 1024, nil)

	preflight, err := CheckRestoreSpace(backup, t.TempDir(), DefaultRestoreSpaceMargin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if preflight.Verdict != SpaceSufficient {
		t.Errorf("expected a 1 KiB restore to fit, got %s (%s)", preflight.Verdict, preflight.Reason)
	}
	if !preflight.Fits() {
		t.Errorf("a sufficient verdict should report Fits() == true")
	}
	if len(preflight.Filesystems) != 1 {
		t.Errorf("expected one filesystem, got %d", len(preflight.Filesystems))
	}
}

func TestCheckRestoreSpace_InsufficientOnSingleFilesystem(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	// Larger than any plausible free space on a test machine.
	backup := putSizedBackup(t, root, testBackupName, 1<<62, nil)

	preflight, err := CheckRestoreSpace(backup, t.TempDir(), DefaultRestoreSpaceMargin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if preflight.Verdict != SpaceInsufficient {
		t.Errorf("expected an unrestorably large backup not to fit, got %s", preflight.Verdict)
	}
	if preflight.Fits() {
		t.Errorf("an insufficient verdict should report Fits() == false")
	}
}

// The margin is the whole reason the check triggers before the disk is literally
// full, so it has to actually enter the arithmetic.
func TestCheckRestoreSpace_MarginIsApplied(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	backup := putSizedBackup(t, root, testBackupName, 1000, nil)

	preflight, err := CheckRestoreSpace(backup, t.TempDir(), 2.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if preflight.NeededBytes != 2500 {
		t.Errorf("expected 1000 bytes at a 2.5x margin to need 2500, got %d", preflight.NeededBytes)
	}
	if preflight.RequiredBytes != 1000 {
		t.Errorf("expected the raw requirement to stay 1000, got %d", preflight.RequiredBytes)
	}
}

func TestCheckRestoreSpace_DefaultMarginWhenUnset(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	backup := putSizedBackup(t, root, testBackupName, 1000, nil)

	preflight, err := CheckRestoreSpace(backup, t.TempDir(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if preflight.Margin != DefaultRestoreSpaceMargin {
		t.Errorf("expected a zero margin to fall back to the default, got %v", preflight.Margin)
	}
}

// A backup from before size recording cannot be sized. That is "unknown", not a
// refusal - blocking those restores would be worse than not checking.
func TestCheckRestoreSpace_UnknownWithoutRecordedSize(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	backup := putSizedBackup(t, root, testBackupName, 0, nil)

	preflight, err := CheckRestoreSpace(backup, t.TempDir(), DefaultRestoreSpaceMargin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if preflight.Verdict != SpaceUnknown {
		t.Errorf("expected an unsized backup to be unknown, got %s", preflight.Verdict)
	}
	if !preflight.Fits() {
		t.Errorf("an unknown verdict must not block the restore")
	}
	if preflight.Reason == "" {
		t.Errorf("an unknown verdict should explain itself")
	}
}

func TestCheckRestoreSpace_UnknownWhenNoPathExists(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	backup := putSizedBackup(t, root, testBackupName, 1024, nil)

	preflight, err := CheckRestoreSpace(backup,
		filepath.Join(t.TempDir(), "does", "not", "exist"), DefaultRestoreSpaceMargin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if preflight.Verdict != SpaceUnknown {
		t.Errorf("expected an absent target path to be unknown, got %s", preflight.Verdict)
	}
}

// Two tablespaces on one filesystem must be counted against one pool of free
// space, not credited with it twice.
func TestCheckRestoreSpace_TablespacesOnSameFilesystemCollapse(t *testing.T) {
	tempDir := t.TempDir()
	tablespaceA := filepath.Join(tempDir, "ts_a")
	tablespaceB := filepath.Join(tempDir, "ts_b")
	mkdirAll(t, tablespaceA)
	mkdirAll(t, tablespaceB)

	root := memory.NewFolder("", memory.NewKVS())
	backup := putSizedBackup(t, root, testBackupName, 1024, map[string]string{
		"16451": tablespaceA,
		"16452": tablespaceB,
	})

	preflight, err := CheckRestoreSpace(backup, tempDir, DefaultRestoreSpaceMargin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(preflight.Filesystems) != 1 {
		t.Fatalf("expected paths on one filesystem to collapse to a single entry, got %d",
			len(preflight.Filesystems))
	}
	if got := len(preflight.Filesystems[0].Paths); got != 3 {
		t.Errorf("expected the data dir and both tablespaces on the entry, got %d paths", got)
	}
	if preflight.Verdict != SpaceSufficient {
		t.Errorf("expected a definitive verdict when everything is on one filesystem, got %s",
			preflight.Verdict)
	}
}

// mkdirAll creates a directory the preflight can measure.
func mkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
}

func TestRestoreTargetPaths_IncludesTablespaces(t *testing.T) {
	spec := NewTablespaceSpec("/var/lib/postgresql/data")
	spec.addTablespace("16451", "/mnt/fast/ts1")
	spec.addTablespace("16452", "/mnt/slow/ts2")

	paths := restoreTargetPaths("/var/lib/postgresql/data", &spec)

	if len(paths) != 3 {
		t.Fatalf("expected the data dir plus two tablespaces, got %v", paths)
	}
	if paths[0] != "/var/lib/postgresql/data" {
		t.Errorf("expected the data directory first, got %q", paths[0])
	}
}

func TestRestoreTargetPaths_NilSpec(t *testing.T) {
	paths := restoreTargetPaths("/var/lib/postgresql/data", nil)

	if len(paths) != 1 || paths[0] != "/var/lib/postgresql/data" {
		t.Errorf("expected just the data directory, got %v", paths)
	}
}

func TestRestorePreflight_SummaryMentionsTheShortfall(t *testing.T) {
	preflight := RestorePreflight{
		BackupName:    "base_00000001",
		Verdict:       SpaceInsufficient,
		RequiredBytes: 10 * 1024 * 1024 * 1024,
		NeededBytes:   12 * 1024 * 1024 * 1024,
		FreeBytes:     2 * 1024 * 1024 * 1024,
	}

	summary := preflight.Summary()
	for _, want := range []string{"2.0 GiB", "12.0 GiB", "base_00000001"} {
		if !bytes.Contains([]byte(summary), []byte(want)) {
			t.Errorf("summary %q does not mention %q", summary, want)
		}
	}
}

func TestRestorePreflight_DescribeBreaksDownMultipleFilesystems(t *testing.T) {
	preflight := RestorePreflight{
		BackupName: "base_00000001",
		Verdict:    SpaceIndeterminate,
		Reason:     "spans 2 filesystems",
		Filesystems: []FilesystemSpace{
			{ID: "dev:1", Paths: []string{"/var/lib/postgresql/data"}, FreeBytes: 1024, TotalBytes: 4096},
			{ID: "dev:2", Paths: []string{"/mnt/fast/ts1"}, FreeBytes: 2048, TotalBytes: 8192},
		},
	}

	described := preflight.Describe()
	for _, want := range []string{"/var/lib/postgresql/data", "/mnt/fast/ts1"} {
		if !bytes.Contains([]byte(described), []byte(want)) {
			t.Errorf("description %q does not mention %q", described, want)
		}
	}
}
