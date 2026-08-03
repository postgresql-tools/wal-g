// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

// explainerOver builds an explainer over a fixed storage snapshot, bypassing the
// storage read so the tests exercise the reporting rather than the listing.
func explainerOver(backups []PITRBackup, segments map[WalSegmentDescription]bool,
	times map[WalSegmentDescription]time.Time) *DeleteExplainer {
	return &DeleteExplainer{
		command: "delete retain 1",
		before: PITRInputs{
			Backups:      backups,
			Segments:     segments,
			SegmentTimes: times,
		},
	}
}

func planOf(names ...string) internal.DeletePlan {
	objects := make([]storage.Object, 0, len(names))
	for _, name := range names {
		objects = append(objects, storage.NewLocalObject(name, pitrEpoch, 1024))
	}
	return internal.DeletePlan{Objects: objects}
}

func explain(t *testing.T, explainer *DeleteExplainer) DeleteExplanation {
	t.Helper()

	explanation, err := explainer.Explain()
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}
	return explanation
}

func hasText(lines []string, substring string) bool {
	for _, line := range lines {
		if strings.Contains(line, substring) {
			return true
		}
	}
	return false
}

func TestExplain_EmptyPlanReportsNothingMatched(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})
	explainer := explainerOver([]PITRBackup{backupAt("base_a", 1, 1, 2)}, segments, times)

	explanation := explain(t, explainer)

	if explanation.Objects != 0 {
		t.Errorf("expected no objects, got %d", explanation.Objects)
	}
	if len(explanation.Warnings) != 0 {
		t.Errorf("an empty plan is not alarming, got %v", explanation.Warnings)
	}
	if !hasText(explanation.Effects, "Nothing matches") {
		t.Errorf("expected a 'nothing matches' effect, got %v", explanation.Effects)
	}
}

func TestExplain_TrimmingOldBackupMovesEarliestForward(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})
	explainer := explainerOver([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 10, 11),
	}, segments, times)

	if err := explainer.Collect(planOf("basebackups_005/base_a/metadata.json")); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if len(explanation.BackupsDeleted) != 1 || explanation.BackupsDeleted[0].Name != "base_a" {
		t.Fatalf("expected base_a to be deleted, got %+v", explanation.BackupsDeleted)
	}
	if len(explanation.BackupsRetained) != 1 || explanation.BackupsRetained[0].Name != "base_b" {
		t.Fatalf("expected base_b to be retained, got %+v", explanation.BackupsRetained)
	}
	if !hasText(explanation.Effects, "earliest restorable point moves forward") {
		t.Errorf("expected the lost history to be reported, got %v", explanation.Effects)
	}
	// Trimming the old end of the window is the point of a retention delete, so
	// it must not be dressed up as a warning.
	if len(explanation.Warnings) != 0 {
		t.Errorf("a routine retention trim should not warn, got %v", explanation.Warnings)
	}
}

func TestExplain_RemovingEverythingRestorableWarns(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})
	explainer := explainerOver([]PITRBackup{backupAt("base_a", 1, 1, 2)}, segments, times)

	if err := explainer.Collect(planOf("basebackups_005/base_a/metadata.json")); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if !explanation.RecoveryWindowAfter.Empty() {
		t.Fatalf("expected nothing restorable afterwards, got %+v", explanation.RecoveryWindowAfter)
	}
	if !hasText(explanation.Warnings, "NOTHING restorable") {
		t.Errorf("expected a warning that nothing survives, got %v", explanation.Warnings)
	}
}

// Deleting a full backup while keeping its deltas leaves objects that cost money
// and can never be restored. That is worth saying loudly, because backup-list
// will keep showing them as if they were usable.
func TestExplain_StrandedDeltaWarns(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})

	base := backupAt("base_a", 1, 1, 2)
	delta := backupAt("base_b_D_base_a", 1, 10, 11)
	delta.IncrementFrom = "base_a"
	delta.IncrementFull = "base_a"

	explainer := explainerOver([]PITRBackup{base, delta}, segments, times)

	if err := explainer.Collect(planOf("basebackups_005/base_a/metadata.json")); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if !hasText(explanation.Warnings, "unrestorable but are NOT deleted") {
		t.Errorf("expected a stranded-backup warning, got %v", explanation.Warnings)
	}
	if !hasText(explanation.Warnings, "base_b_D_base_a") {
		t.Errorf("the warning should name the stranded backup, got %v", explanation.Warnings)
	}
}

// Retention deletes trim the far end of the window. Losing the recent end means
// recently archived WAL is going, which is almost never what was meant.
func TestExplain_LatestRestorablePointMovingBackWarns(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 10})
	explainer := explainerOver([]PITRBackup{backupAt("base_a", 1, 1, 2)}, segments, times)

	if err := explainer.Collect(planOf(
		"wal_005/"+WalSegmentNo(8).GetFilename(1)+".lz4",
		"wal_005/"+WalSegmentNo(9).GetFilename(1)+".lz4",
		"wal_005/"+WalSegmentNo(10).GetFilename(1)+".lz4",
	)); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if explanation.WalSegments != 3 {
		t.Errorf("expected 3 WAL segments in the plan, got %d", explanation.WalSegments)
	}
	if !hasText(explanation.Warnings, "moves BACK") {
		t.Errorf("expected a warning about losing recent WAL, got %v", explanation.Warnings)
	}
}

func TestExplain_NewGapWarns(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})
	explainer := explainerOver([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 15, 16),
	}, segments, times)

	// Removing a segment in the middle splits one continuous window in two.
	if err := explainer.Collect(planOf(
		"wal_005/" + WalSegmentNo(8).GetFilename(1) + ".lz4",
	)); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if len(explanation.RecoveryWindowAfter.Gaps) == 0 {
		t.Fatalf("expected a gap to open, got %+v", explanation.RecoveryWindowAfter)
	}
	if !hasText(explanation.Warnings, "new gap") {
		t.Errorf("expected a new-gap warning, got %v", explanation.Warnings)
	}
}

func TestExplain_PermanentBackupInScopeWarns(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})

	permanent := backupAt("base_a", 1, 1, 2)
	permanent.IsPermanent = true

	explainer := explainerOver([]PITRBackup{permanent, backupAt("base_b", 1, 10, 11)}, segments, times)

	if err := explainer.Collect(planOf("basebackups_005/base_a/metadata.json")); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if !hasText(explanation.Warnings, "permanent backup(s) are in scope") {
		t.Errorf("expected a permanent-backup warning, got %v", explanation.Warnings)
	}
}

func TestExplain_ReportsReclaimedBytes(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})
	explainer := explainerOver([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 10, 11),
	}, segments, times)

	if err := explainer.Collect(planOf(
		"basebackups_005/base_a/metadata.json",
		"basebackups_005/base_a/tar_partitions/part_001.tar.lz4",
	)); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if explanation.Objects != 2 {
		t.Errorf("expected 2 objects, got %d", explanation.Objects)
	}
	if explanation.Bytes != 2048 {
		t.Errorf("expected 2048 bytes, got %d", explanation.Bytes)
	}
	if !hasText(explanation.Effects, "Reclaims") {
		t.Errorf("expected the reclaimed space to be reported, got %v", explanation.Effects)
	}
}

// Plans arrive per subfolder; "delete target" lists inside the base backup
// folder, so its object names only identify a backup once the prefix is put back.
func TestExplain_PlanPrefixIsAppliedToObjectNames(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})
	explainer := explainerOver([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 10, 11),
	}, segments, times)

	plan := planOf("base_a/metadata.json")
	plan.Prefix = "basebackups_005/"

	if err := explainer.Collect(plan); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	if len(explanation.BackupsDeleted) != 1 || explanation.BackupsDeleted[0].Name != "base_a" {
		t.Errorf("expected base_a to be recognised through the prefix, got %+v", explanation.BackupsDeleted)
	}
}

func TestExplain_JSONOutputRoundTrips(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})
	explainer := explainerOver([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 10, 11),
	}, segments, times)

	if err := explainer.Collect(planOf("basebackups_005/base_a/metadata.json")); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	buf := &bytes.Buffer{}
	if err := WriteExplanation(&explanation, "json", buf); err != nil {
		t.Fatalf("WriteExplanation failed: %v", err)
	}

	var parsed DeleteExplanation
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("explain JSON does not parse: %v\noutput: %s", err, buf.String())
	}
	if len(parsed.BackupsDeleted) != 1 || parsed.BackupsDeleted[0].Name != "base_a" {
		t.Errorf("expected base_a in the parsed output, got %+v", parsed.BackupsDeleted)
	}
}

// The text report is what an operator reads before adding --confirm, so it has
// to say that nothing has happened yet.
func TestExplain_TextOutputSaysNothingWasDeleted(t *testing.T) {
	segments, times := segmentSet(1, [2]uint64{1, 20})
	explainer := explainerOver([]PITRBackup{
		backupAt("base_a", 1, 1, 2),
		backupAt("base_b", 1, 10, 11),
	}, segments, times)

	if err := explainer.Collect(planOf("basebackups_005/base_a/metadata.json")); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	explanation := explain(t, explainer)

	buf := &bytes.Buffer{}
	if err := WriteExplanation(&explanation, "text", buf); err != nil {
		t.Fatalf("WriteExplanation failed: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"Nothing was deleted", "--confirm", "Recovery window", "base_a"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected the report to mention %q:\n%s", want, output)
		}
	}
}
