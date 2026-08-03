// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"testing"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/memory"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

// putChainFull writes a full backup: no increment fields at all.
func putChainFull(t *testing.T, root storage.Folder, name string) {
	t.Helper()

	putChainSentinel(t, root, name, BackupSentinelDto{
		BackupStartLSN:  lsnPtr(0x1000000),
		BackupFinishLSN: lsnPtr(0x2000000),
		PgVersion:       150000,
	})
}

// putChainDelta writes a delta backup incrementing from parent.
func putChainDelta(t *testing.T, root storage.Folder, name, parent, full string, count int) {
	t.Helper()

	putChainSentinel(t, root, name, BackupSentinelDto{
		BackupStartLSN:    lsnPtr(0x1000000),
		BackupFinishLSN:   lsnPtr(0x2000000),
		PgVersion:         150000,
		IncrementFrom:     strPtr(parent),
		IncrementFromLSN:  lsnPtr(0x900000),
		IncrementFullName: strPtr(full),
		IncrementCount:    intPtr(count),
	})
}

func putChainSentinel(t *testing.T, root storage.Folder, name string, sentinel BackupSentinelDto) {
	t.Helper()

	if err := root.PutObject(utility.BaseBackupPath+internal.SentinelNameFromBackup(name),
		bytes.NewReader(mustMarshal(t, sentinel))); err != nil {
		t.Fatalf("failed to write sentinel for %s: %v", name, err)
	}
}

func resolveChain(t *testing.T, root storage.Folder, name string) DeltaChain {
	t.Helper()

	chain, err := ResolveDeltaChain(root.GetSubFolder(utility.BaseBackupPath), name)
	if err != nil {
		t.Fatalf("ResolveDeltaChain failed: %v", err)
	}
	return chain
}

func TestDeltaChain_FullBackupHasDepthZero(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putChainFull(t, root, "base_full")

	chain := resolveChain(t, root, "base_full")

	if chain.Broken {
		t.Fatalf("chain should not be broken: %+v", chain)
	}
	if chain.Depth != 0 {
		t.Errorf("expected depth 0, got %d", chain.Depth)
	}
	if chain.FullBackupName != "base_full" {
		t.Errorf("expected base_full at the base, got %q", chain.FullBackupName)
	}
}

func TestDeltaChain_DepthCountsDeltaLinks(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putChainFull(t, root, "base_full")
	putChainDelta(t, root, "base_d1", "base_full", "base_full", 1)
	putChainDelta(t, root, "base_d2", "base_d1", "base_full", 2)
	putChainDelta(t, root, "base_d3", "base_d2", "base_full", 3)

	chain := resolveChain(t, root, "base_d3")

	if chain.Broken {
		t.Fatalf("chain should not be broken: %+v", chain)
	}
	if chain.Depth != 3 {
		t.Errorf("expected depth 3, got %d", chain.Depth)
	}
	if chain.FullBackupName != "base_full" {
		t.Errorf("expected base_full at the base, got %q", chain.FullBackupName)
	}
	// Oldest first, so the chain reads the way a restore would apply it.
	want := []string{"base_full", "base_d1", "base_d2", "base_d3"}
	if len(chain.Links) != len(want) {
		t.Fatalf("expected %v, got %v", want, chain.Links)
	}
	for i := range want {
		if chain.Links[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, chain.Links)
		}
	}
}

// The whole point of walking storage: a sentinel can claim a depth that the
// links do not support, and the walk must act on the links.
func TestDeltaChain_WalkedDepthBeatsRecordedCount(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putChainFull(t, root, "base_full")
	putChainDelta(t, root, "base_d1", "base_full", "base_full", 1)
	// This delta claims to be the first in its chain while sitting fourth.
	putChainDelta(t, root, "base_d2", "base_d1", "base_full", 1)
	putChainDelta(t, root, "base_d3", "base_d2", "base_full", 1)

	chain := resolveChain(t, root, "base_d3")

	if chain.Depth != 3 {
		t.Fatalf("expected the walked depth 3, got %d", chain.Depth)
	}

	// With a limit of 3, the recorded count would have allowed a fourth delta.
	decision := DecideDelta(DeltaDecisionInput{
		Chain:           chain,
		RecordedCount:   intPtr(1),
		MaxDeltas:       3,
		BaseHasStartLSN: true,
	})

	if decision.UseDelta {
		t.Errorf("expected promotion to full, got a delta at depth %d", decision.Depth)
	}
	if decision.Reason != DeltaPromotionMaxDepth {
		t.Errorf("expected reason %q, got %q", DeltaPromotionMaxDepth, decision.Reason)
	}
}

func TestDeltaChain_MissingLinkBreaksTheChain(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	// base_full is never written, so base_d1 depends on something absent.
	putChainDelta(t, root, "base_d1", "base_full", "base_full", 1)

	chain := resolveChain(t, root, "base_d1")

	if !chain.Broken {
		t.Fatalf("expected a broken chain, got %+v", chain)
	}
	if chain.BreakReason != DeltaPromotionChainBroken {
		t.Errorf("expected reason %q, got %q", DeltaPromotionChainBroken, chain.BreakReason)
	}
}

func TestDeltaChain_CycleIsDetected(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putChainDelta(t, root, "base_a", "base_b", "base_a", 1)
	putChainDelta(t, root, "base_b", "base_a", "base_a", 2)

	chain := resolveChain(t, root, "base_a")

	if !chain.Broken {
		t.Fatalf("expected a broken chain, got %+v", chain)
	}
	if chain.BreakReason != DeltaPromotionChainCycle {
		t.Errorf("expected reason %q, got %q", DeltaPromotionChainCycle, chain.BreakReason)
	}
}

// A sentinel naming an increment base without the rest of its increment fields
// makes IsIncremental panic. The walk must report it, not trip over it.
func TestDeltaChain_InconsistentSentinelIsReportedNotPanicked(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putChainFull(t, root, "base_full")
	putChainSentinel(t, root, "base_bad", BackupSentinelDto{
		BackupStartLSN:  lsnPtr(0x1000000),
		BackupFinishLSN: lsnPtr(0x2000000),
		PgVersion:       150000,
		IncrementFrom:   strPtr("base_full"),
		// IncrementFromLSN, IncrementFullName and IncrementCount are all absent.
	})

	chain := resolveChain(t, root, "base_bad")

	if !chain.Broken {
		t.Fatalf("expected a broken chain, got %+v", chain)
	}
	if chain.BreakReason != DeltaPromotionChainUnreadable {
		t.Errorf("expected reason %q, got %q", DeltaPromotionChainUnreadable, chain.BreakReason)
	}
}

func TestDecideDelta_UnderTheLimitTakesADelta(t *testing.T) {
	decision := DecideDelta(DeltaDecisionInput{
		Chain:           DeltaChain{Depth: 1, Links: []string{"base_full", "base_d1"}},
		MaxDeltas:       3,
		BaseHasStartLSN: true,
	})

	if !decision.UseDelta {
		t.Fatalf("expected a delta, got promotion: %s", decision.Reason)
	}
	if decision.Depth != 2 {
		t.Errorf("expected depth 2, got %d", decision.Depth)
	}
	if decision.Promoted() {
		t.Error("a delta is not a promotion")
	}
}

// The boundary that upstream sets with `incrementCount > maxDeltas`: a chain at
// the limit promotes, one below it does not.
func TestDecideDelta_LimitBoundaryMatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		depth     int
		max       int
		wantDelta bool
	}{
		{depth: 0, max: 1, wantDelta: true},
		{depth: 1, max: 1, wantDelta: false},
		{depth: 2, max: 3, wantDelta: true},
		{depth: 3, max: 3, wantDelta: false},
		{depth: 4, max: 3, wantDelta: false},
	} {
		decision := DecideDelta(DeltaDecisionInput{
			Chain:           DeltaChain{Depth: tc.depth},
			MaxDeltas:       tc.max,
			BaseHasStartLSN: true,
		})

		if decision.UseDelta != tc.wantDelta {
			t.Errorf("depth %d limit %d: expected useDelta=%v, got %v (%s)",
				tc.depth, tc.max, tc.wantDelta, decision.UseDelta, decision.Reason)
		}
	}
}

func TestDecideDelta_BrokenChainPromotes(t *testing.T) {
	decision := DecideDelta(DeltaDecisionInput{
		Chain: DeltaChain{
			Broken:      true,
			BreakReason: DeltaPromotionChainBroken,
			BreakDetail: "base_full is referenced by the chain but is not in storage",
		},
		MaxDeltas:       10,
		BaseHasStartLSN: true,
	})

	if decision.UseDelta {
		t.Fatal("expected promotion when the chain is broken")
	}
	if decision.Reason != DeltaPromotionChainBroken {
		t.Errorf("expected reason %q, got %q", DeltaPromotionChainBroken, decision.Reason)
	}
	if decision.Detail == "" {
		t.Error("a promotion should carry the detail that explains it")
	}
}

func TestDecideDelta_BaseWithoutStartLSNPromotes(t *testing.T) {
	decision := DecideDelta(DeltaDecisionInput{
		Chain:           DeltaChain{Depth: 0},
		MaxDeltas:       5,
		BaseHasStartLSN: false,
	})

	if decision.UseDelta {
		t.Fatal("expected promotion when the base records no start LSN")
	}
	if decision.Reason != DeltaPromotionBaseWithoutLSN {
		t.Errorf("expected reason %q, got %q", DeltaPromotionBaseWithoutLSN, decision.Reason)
	}
}

func TestDecideDelta_PermanentBase(t *testing.T) {
	base := DeltaDecisionInput{
		Chain:           DeltaChain{Depth: 0},
		MaxDeltas:       5,
		BaseHasStartLSN: true,
		BaseIsPermanent: true,
	}

	if decision := DecideDelta(base); decision.Reason != DeltaPromotionBasePermanent {
		t.Errorf("expected promotion off a permanent base, got %+v", decision)
	}

	// A permanent backup may increment from a permanent base, as upstream allows.
	permanentNext := base
	permanentNext.NextIsPermanent = true
	if decision := DecideDelta(permanentNext); !decision.UseDelta {
		t.Errorf("expected a permanent delta to be allowed, got %s", decision.Reason)
	}

	// LATEST_FULL rebases onto the full backup, which upstream also allows.
	fromFull := base
	fromFull.FromFull = true
	if decision := DecideDelta(fromFull); !decision.UseDelta {
		t.Errorf("expected LATEST_FULL to be allowed off a permanent base, got %s", decision.Reason)
	}
}

// Under LATEST_FULL every delta is rebased onto the full backup, so the walked
// depth is always 1 and could never reach the limit. There the limit bounds how
// many deltas accumulate on one full backup, which is the recorded count.
func TestDecideDelta_LatestFullCountsDeltasSinceTheFullBackup(t *testing.T) {
	input := DeltaDecisionInput{
		Chain:           DeltaChain{Depth: 1, Links: []string{"base_full", "base_d3"}},
		RecordedCount:   intPtr(3),
		MaxDeltas:       3,
		FromFull:        true,
		BaseHasStartLSN: true,
	}

	decision := DecideDelta(input)

	if decision.UseDelta {
		t.Fatalf("expected promotion once the limit is reached, got depth %d", decision.Depth)
	}
	if decision.Reason != DeltaPromotionMaxDepth {
		t.Errorf("expected reason %q, got %q", DeltaPromotionMaxDepth, decision.Reason)
	}

	// One below the limit still increments.
	input.RecordedCount = intPtr(2)
	if decision := DecideDelta(input); !decision.UseDelta || decision.Depth != 3 {
		t.Errorf("expected a delta at depth 3, got useDelta=%v depth=%d",
			decision.UseDelta, decision.Depth)
	}
}
