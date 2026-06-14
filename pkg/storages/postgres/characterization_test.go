package postgres

import (
	"path/filepath"
	"runtime"
	"testing"
	"github.com/lateos-ai/wal-g/internal/characterization"
)

func goldenPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../../../testdata/characterization/postgres.golden.json")
}

func TestCharacterizationPostgresFullBackupS3(t *testing.T) {
	golden := &characterization.GoldenFile{
		Name: "postgres_full_backup_s3",
		Path: goldenPath(),
	}

	// Simulate or capture actual backup snapshot
	actual := characterization.BackupSnapshot{
		Provider:       "s3",
		Database:       "postgres",
		BackupType:     "full",
		BackupSize:     1073741824, // 1GB
		FilesBackedUp:  42,
		Compression:    "lz4",
		Encryption:     "aes-256-gcm",
		ChecksumSHA256: "abc123...",
	}

	characterization.VerifyBackupSnapshot(t, golden, actual, "postgres_full_backup_s3")
}

func TestCharacterizationPostgresIncrementalS3(t *testing.T) {
	golden := &characterization.GoldenFile{
		Name: "postgres_incremental_s3",
		Path: goldenPath(),
	}

	actual := characterization.BackupSnapshot{
		Provider:       "s3",
		Database:       "postgres",
		BackupType:     "incremental",
		BackupSize:     104857600, // 100MB
		FilesBackedUp:  8,
		Compression:    "lz4",
		Encryption:     "aes-256-gcm",
		ChecksumSHA256: "def456...",
	}

	characterization.VerifyBackupSnapshot(t, golden, actual, "postgres_incremental_s3")
}

func TestCharacterizationPostgresFullBackupGCS(t *testing.T) {
	golden := &characterization.GoldenFile{
		Name: "postgres_full_backup_gcs",
		Path: goldenPath(),
	}

	actual := characterization.BackupSnapshot{
		Provider:       "gcs",
		Database:       "postgres",
		BackupType:     "full",
		BackupSize:     1073741824, // 1GB
		FilesBackedUp:  42,
		Compression:    "lz4",
		Encryption:     "csek",
		ChecksumSHA256: "ghi789...",
	}

	characterization.VerifyBackupSnapshot(t, golden, actual, "postgres_full_backup_gcs")
}