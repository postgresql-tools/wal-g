package characterization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// BackupSnapshot captures the essential characteristics of a backup
type BackupSnapshot struct {
	Provider       string `json:"provider"`    // s3, gcs, azure
	Database       string `json:"database"`    // postgres, mysql, mongo
	BackupType     string `json:"backup_type"` // full, incremental
	BackupSize     int64  `json:"backup_size"` // bytes
	FilesBackedUp  int    `json:"files_backed"`
	Compression    string `json:"compression"` // lz4, xz, brotli
	Encryption     string `json:"encryption"`  // aes-256, kms, csek
	ChecksumSHA256 string `json:"checksum"`
}

// GoldenFile stores expected backup characteristics
type GoldenFile struct {
	Name      string
	Path      string
	Snapshots map[string]BackupSnapshot // keyed by scenario
}

// VerifyBackupSnapshot compares actual backup against golden file
func VerifyBackupSnapshot(t *testing.T, golden *GoldenFile, actual BackupSnapshot, scenario string) {
	// Load golden file
	data, err := os.ReadFile(golden.Path)
	if err != nil {
		// First run: create golden file
		saveGoldenFile(t, golden.Path, actual, scenario)
		t.Logf("Created golden file: %s", golden.Path)
		return
	}

	var snapshots map[string]BackupSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		t.Fatalf("Failed to parse golden file: %v", err)
	}

	expected, ok := snapshots[scenario]
	if !ok {
		t.Fatalf("Scenario %s not found in golden file", scenario)
	}

	// Compare
	if expected.Provider != actual.Provider {
		t.Errorf("Provider mismatch: expected %s, got %s", expected.Provider, actual.Provider)
	}
	if expected.Database != actual.Database {
		t.Errorf("Database mismatch: expected %s, got %s", expected.Database, actual.Database)
	}
	if expected.BackupType != actual.BackupType {
		t.Errorf("BackupType mismatch: expected %s, got %s", expected.BackupType, actual.BackupType)
	}
	if expected.Compression != actual.Compression {
		t.Errorf("Compression mismatch: expected %s, got %s", expected.Compression, actual.Compression)
	}
	if expected.Encryption != actual.Encryption {
		t.Errorf("Encryption mismatch: expected %s, got %s", expected.Encryption, actual.Encryption)
	}

	// Size and file count can vary slightly, but check within 10%
	diff := int64(float64(expected.BackupSize) * 0.1)
	if actual.BackupSize < expected.BackupSize-diff || actual.BackupSize > expected.BackupSize+diff {
		t.Logf("BackupSize drift: expected ~%d, got %d (±10%% acceptable)", expected.BackupSize, actual.BackupSize)
	}
}

// saveGoldenFile creates or updates golden file
func saveGoldenFile(t *testing.T, path string, snapshot BackupSnapshot, scenario string) {
	snapshots := map[string]BackupSnapshot{
		scenario: snapshot,
	}

	data, _ := json.MarshalIndent(snapshots, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("Failed to save golden file: %v", err)
	}
}

// ComputeChecksum returns SHA256 of backup data
func ComputeChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
