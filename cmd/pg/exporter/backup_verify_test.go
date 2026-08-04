package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func newTestExporter() *WalgExporter {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewWalgExporter(logger, "wal-g", time.Minute, time.Minute, time.Minute, time.Minute, 0, "")
}

func TestCoverageRatio(t *testing.T) {
	tests := []struct {
		name     string
		coverage ChecksumCoverageData
		want     float64
	}{
		{"full coverage", ChecksumCoverageData{HasChecksum: 4, Total: 4}, 1.0},
		{"partial coverage", ChecksumCoverageData{HasChecksum: 3, NoChecksum: 1, Total: 4}, 0.75},
		{"no coverage", ChecksumCoverageData{NoChecksum: 4, Total: 4}, 0.0},
		{"no files recorded", ChecksumCoverageData{}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, tt.coverage.CoverageRatio(), 0.0001)
		})
	}
}

func TestUpdateBackupVerifyMetrics_FailingBackup(t *testing.T) {
	data, err := os.ReadFile("testdata/backup-verify.json")
	require.NoError(t, err)

	var verifyData BackupVerifyResponse
	require.NoError(t, json.Unmarshal(data, &verifyData))

	e := newTestExporter()
	e.updateBackupVerifyMetrics(&verifyData)

	const name = "base_000000430000038500000003"

	require.Equal(t, 0.0, testutil.ToFloat64(e.backupVerifyStatus.WithLabelValues(name, "1")),
		"a failing backup should report status 0")
	require.Equal(t, 2.0, testutil.ToFloat64(e.backupVerifyMissingParts.WithLabelValues(name)))
	require.InDelta(t, 0.75, testutil.ToFloat64(e.backupChecksumCoverage.WithLabelValues(name)), 0.0001)
	require.Equal(t, 0.0, testutil.ToFloat64(e.backupVerifyDecryptCanary.WithLabelValues(name, "libsodium")),
		"an attempted canary that failed should report 0")
	require.Positive(t, testutil.ToFloat64(e.backupVerifyTimestamp.WithLabelValues(name)))
}

func TestUpdateBackupVerifyMetrics_PassingBackup(t *testing.T) {
	verifyData := &BackupVerifyResponse{
		BackupName: "base_00000001000000000000000A",
		Tier:       "2",
		Pass:       true,
		ChecksumCoverage: ChecksumCoverageData{
			HasChecksum: 10,
			Total:       10,
		},
		DecryptCanary: DecryptCanaryData{
			Attempted: true,
			Pass:      true,
			Crypter:   "none",
		},
	}

	e := newTestExporter()
	e.updateBackupVerifyMetrics(verifyData)

	const name = "base_00000001000000000000000A"

	require.Equal(t, 1.0, testutil.ToFloat64(e.backupVerifyStatus.WithLabelValues(name, "2")))
	require.Equal(t, 0.0, testutil.ToFloat64(e.backupVerifyMissingParts.WithLabelValues(name)))
	require.Equal(t, 1.0, testutil.ToFloat64(e.backupChecksumCoverage.WithLabelValues(name)))
	require.Equal(t, 1.0, testutil.ToFloat64(e.backupVerifyDecryptCanary.WithLabelValues(name, "none")))
}

// A skipped canary must stay distinguishable from a failed one: alerting on
// "== 0" would otherwise fire on backups nobody ever checked.
func TestUpdateBackupVerifyMetrics_SkippedCanaryIsNotAFailure(t *testing.T) {
	verifyData := &BackupVerifyResponse{
		BackupName:    "base_00000001000000000000000B",
		Tier:          "1",
		Pass:          true,
		DecryptCanary: DecryptCanaryData{Attempted: false},
	}

	e := newTestExporter()
	e.updateBackupVerifyMetrics(verifyData)

	require.Equal(t, -1.0,
		testutil.ToFloat64(e.backupVerifyDecryptCanary.WithLabelValues("base_00000001000000000000000B", "unknown")))
}

func TestUpdateBackupVerifyMetrics_ResetsStaleSeries(t *testing.T) {
	e := newTestExporter()

	e.updateBackupVerifyMetrics(&BackupVerifyResponse{
		BackupName: "base_old",
		Tier:       "1",
		Pass:       true,
	})
	e.updateBackupVerifyMetrics(&BackupVerifyResponse{
		BackupName: "base_new",
		Tier:       "1",
		Pass:       true,
	})

	require.Equal(t, 1, testutil.CollectAndCount(e.backupVerifyStatus),
		"the previous backup's series should not linger after a newer scrape")
}
