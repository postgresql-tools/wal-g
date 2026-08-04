// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/lateos-ai/wal-g/pkg/storages/memory"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

func runDoctorJSON(t *testing.T, root storage.Folder, opts DoctorOptions) (DoctorResult, int) {
	t.Helper()

	opts.Format = "json"
	buf := &bytes.Buffer{}
	code := HandleDoctor(root, opts, buf)

	var result DoctorResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse doctor JSON output: %v\noutput: %s", err, buf.String())
	}
	return result, code
}

func findCheck(t *testing.T, result DoctorResult, name string) DoctorCheck {
	t.Helper()

	for _, c := range result.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("doctor report has no %q check", name)
	return DoctorCheck{}
}

func TestDoctor_StorageRoundTripPasses(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	result, _ := runDoctorJSON(t, root, DoctorOptions{SkipPG: true})

	check := findCheck(t, result, DoctorCheckStorage)
	if check.Status != DoctorPass {
		t.Errorf("expected storage check to pass, got %s: %s (%s)", check.Status, check.Summary, check.Detail)
	}
}

// The probe object must not survive the check: leaving debris in a user's bucket
// on every run would be its own bug report.
func TestDoctor_StorageCheckCleansUpProbeObject(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	runDoctorJSON(t, root, DoctorOptions{SkipPG: true})

	objects, _, err := root.ListFolder()
	if err != nil {
		t.Fatalf("failed to list storage: %v", err)
	}
	for _, obj := range objects {
		if bytes.Contains([]byte(obj.GetName()), []byte("doctor-canary")) {
			t.Errorf("doctor left a probe object behind: %s", obj.GetName())
		}
	}
}

func TestDoctor_NoBackupsFails(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	result, code := runDoctorJSON(t, root, DoctorOptions{SkipPG: true})

	check := findCheck(t, result, DoctorCheckBackups)
	if check.Status != DoctorFail {
		t.Errorf("expected backups check to fail with an empty storage, got %s", check.Status)
	}
	if check.Remedy == "" {
		t.Errorf("a failing check should say what to do about it")
	}
	if code != 1 {
		t.Errorf("expected exit code 1 when a check fails, got %d", code)
	}
	if result.Pass {
		t.Errorf("expected overall result to be not-ready")
	}
}

func TestDoctor_FreshBackupPasses(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putDoctorBackup(t, root, testBackupName, 1024)

	result, _ := runDoctorJSON(t, root, DoctorOptions{SkipPG: true})

	check := findCheck(t, result, DoctorCheckBackups)
	if check.Status != DoctorPass {
		t.Errorf("expected backups check to pass for a fresh backup, got %s: %s", check.Status, check.Summary)
	}
}

// A stale backup is a warning, not a failure: it is a schedule problem, not a
// reason to say the host cannot restore.
func TestDoctor_StaleBackupWarnsButDoesNotFail(t *testing.T) {
	// The memory KVS rounds timestamps up to the next microsecond, so a backup
	// written "now" can read as zero seconds old. Pin the write time instead.
	threeDaysAgo := time.Now().Add(-72 * time.Hour)
	root := memory.NewFolder("", memory.NewKVS(memory.WithCustomTime(func() time.Time {
		return threeDaysAgo
	})))
	putDoctorBackup(t, root, testBackupName, 1024)

	result, code := runDoctorJSON(t, root, DoctorOptions{
		SkipPG:     true,
		StaleAfter: 26 * time.Hour,
	})

	check := findCheck(t, result, DoctorCheckBackups)
	if check.Status != DoctorWarn {
		t.Errorf("expected a stale backup to warn, got %s: %s", check.Status, check.Summary)
	}
	if code != 0 {
		t.Errorf("expected warnings not to affect the exit code, got %d", code)
	}
}

func TestDoctor_SkipPGSkipsPostgresChecks(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())

	result, _ := runDoctorJSON(t, root, DoctorOptions{SkipPG: true})

	for _, name := range []string{DoctorCheckPostgres, DoctorCheckArchiving} {
		if check := findCheck(t, result, name); check.Status != DoctorSkip {
			t.Errorf("expected %s check to be skipped, got %s", name, check.Status)
		}
	}
}

func TestDoctor_RestoreSpaceSkippedWithoutDataDir(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putDoctorBackup(t, root, testBackupName, 1024)

	result, _ := runDoctorJSON(t, root, DoctorOptions{
		SkipPG:  true,
		DataDir: "/nonexistent/path/for/doctor/test",
	})

	check := findCheck(t, result, DoctorCheckRestoreSpace)
	if check.Status != DoctorSkip {
		t.Errorf("expected restore-space to skip when the data directory is absent, got %s", check.Status)
	}
}

// The margin has to actually bind, or the check would pass right up to the point
// where the restore fills the disk.
func TestDoctor_RestoreSpaceFailsWhenBackupExceedsDisk(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	// Larger than any plausible free space on a test machine.
	putDoctorBackup(t, root, testBackupName, 1<<62)

	result, code := runDoctorJSON(t, root, DoctorOptions{
		SkipPG:  true,
		DataDir: t.TempDir(),
	})

	check := findCheck(t, result, DoctorCheckRestoreSpace)
	if check.Status != DoctorFail {
		t.Fatalf("expected restore-space to fail for an unrestorably large backup, got %s: %s",
			check.Status, check.Summary)
	}
	if check.Remedy == "" {
		t.Errorf("a failing restore-space check should say what to do about it")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestDoctor_RestoreSpacePassesForSmallBackup(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putDoctorBackup(t, root, testBackupName, 1024)

	result, _ := runDoctorJSON(t, root, DoctorOptions{
		SkipPG:  true,
		DataDir: t.TempDir(),
	})

	check := findCheck(t, result, DoctorCheckRestoreSpace)
	if check.Status != DoctorPass {
		t.Errorf("expected restore-space to pass for a 1 KiB backup, got %s: %s (%s)",
			check.Status, check.Summary, check.Detail)
	}
}

func TestDoctor_TextOutputReportsEveryCheck(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putDoctorBackup(t, root, testBackupName, 1024)

	buf := &bytes.Buffer{}
	HandleDoctor(root, DoctorOptions{SkipPG: true, Format: "text"}, buf)

	for _, name := range []string{
		DoctorCheckConfig, DoctorCheckStorage, DoctorCheckEncryption,
		DoctorCheckPostgres, DoctorCheckArchiving, DoctorCheckBackups, DoctorCheckRestoreSpace,
	} {
		if !bytes.Contains(buf.Bytes(), []byte(name)) {
			t.Errorf("text output does not mention the %q check:\n%s", name, buf.String())
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}

	for _, tt := range tests {
		if got := formatBytes(tt.bytes); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h30m"},
		{50 * time.Hour, "2d2h"},
	}

	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatPgVersion(t *testing.T) {
	tests := []struct {
		version int
		want    string
	}{
		{0, "(unknown version)"},
		{150004, "15.4"},
		{180001, "18.1"},
		{90624, "9.6.24"},
	}

	for _, tt := range tests {
		if got := formatPgVersion(tt.version); got != tt.want {
			t.Errorf("formatPgVersion(%d) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

// putDoctorBackup writes a backup whose sentinel records uncompressedSize, which
// is what the restore-space check sizes itself against.
func putDoctorBackup(t *testing.T, root storage.Folder, name string, uncompressedSize int64) {
	t.Helper()

	sentinel := BackupSentinelDto{
		BackupStartLSN:   lsnPtr(0x1000000),
		BackupFinishLSN:  lsnPtr(0x2000000),
		PgVersion:        15,
		UncompressedSize: uncompressedSize,
	}

	data, err := json.Marshal(sentinel)
	if err != nil {
		t.Fatalf("failed to marshal sentinel: %v", err)
	}

	putBackupSentinel(root, name, data)

	filesMeta := minimalFilesMeta()
	putFilesMeta(root, name, filesMeta)
	putTarParts(root, name, realTarParts(filesMeta))

	if err := root.PutObject(
		utility.BaseBackupPath+name+"/"+utility.MetadataFileName,
		bytes.NewReader(mustMarshal(t, ExtendedMetadataDto{
			StartTime:  time.Now(),
			FinishTime: time.Now(),
		})),
	); err != nil {
		t.Fatalf("failed to write backup metadata: %v", err)
	}
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}
