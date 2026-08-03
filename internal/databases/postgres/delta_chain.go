// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"fmt"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

// DeltaPromotionReason says why a backup that could have been a delta is being
// taken as a full backup instead.
//
// Only cases where a delta was actually possible get a reason. "Deltas are
// switched off" and "there is no previous backup" produce full backups too, but
// recording those would put a reason on almost every full backup and drown the
// cases worth auditing.
type DeltaPromotionReason string

const (
	// DeltaPromotionMaxDepth means the chain has reached WALG_DELTA_MAX_STEPS.
	DeltaPromotionMaxDepth DeltaPromotionReason = "chain_at_max_depth"
	// DeltaPromotionChainBroken means a backup the chain depends on is not in
	// storage, so extending the chain would build on something unrestorable.
	DeltaPromotionChainBroken DeltaPromotionReason = "chain_broken"
	// DeltaPromotionChainCycle means the chain's links refer back to each other.
	DeltaPromotionChainCycle DeltaPromotionReason = "chain_cycle"
	// DeltaPromotionChainUnreadable means a link's sentinel could not be read, so
	// the true depth is unknown.
	DeltaPromotionChainUnreadable DeltaPromotionReason = "chain_unreadable"
	// DeltaPromotionBasePermanent means the candidate base is a permanent backup.
	DeltaPromotionBasePermanent DeltaPromotionReason = "base_is_permanent"
	// DeltaPromotionBaseWithoutLSN means the candidate base predates the delta
	// feature and records no start LSN to increment from.
	DeltaPromotionBaseWithoutLSN DeltaPromotionReason = "base_without_lsn"
)

// maxChainWalk bounds the chain walk. WALG_DELTA_MAX_STEPS should stop it long
// before this, but the walk reads names out of storage, and storage can contain
// anything - a bound here is what keeps a malformed chain from hanging a backup.
const maxChainWalk = 1000

// DeltaChain is the increment chain a backup sits at the end of, resolved by
// walking storage rather than by trusting any single backup's own count.
//
// The count in a sentinel is written once and never revisited. If it is missing
// - an older backup, a partial write, a backup copied in from elsewhere - the
// configurator that trusts it restarts counting from one, and the chain grows
// past its limit without anything saying so. Walking the links costs one
// sentinel read per link, once per backup-push, and cannot drift.
type DeltaChain struct {
	// Links are the backups from the base through to the backup the chain was
	// resolved from, oldest first.
	Links []string
	// Depth is how many delta backups stand between the full backup and the end
	// of the chain. A full backup on its own has depth 0.
	Depth int
	// FullBackupName is the full backup at the base. Empty when the chain is broken.
	FullBackupName string
	// Broken is set when the chain does not terminate at a full backup that is
	// present and readable in storage.
	Broken bool
	// BreakReason and BreakDetail describe what went wrong when Broken is set.
	BreakReason DeltaPromotionReason
	BreakDetail string
}

// ResolveDeltaChain walks the increment chain ending at backupName back to the
// full backup it is built on.
func ResolveDeltaChain(baseBackupFolder storage.Folder, backupName string) (DeltaChain, error) {
	chain := DeltaChain{}

	seen := make(map[string]bool)
	current := backupName

	for range maxChainWalk {
		if seen[current] {
			chain.Broken = true
			chain.BreakReason = DeltaPromotionChainCycle
			chain.BreakDetail = fmt.Sprintf("%s appears twice in its own increment chain", current)
			return chain, nil
		}
		seen[current] = true

		// Prepend: the walk runs newest to oldest, the chain reads oldest first.
		chain.Links = append([]string{current}, chain.Links...)

		backup, err := NewBackup(baseBackupFolder, current)
		if err != nil {
			chain.Broken = true
			chain.BreakReason = DeltaPromotionChainUnreadable
			chain.BreakDetail = fmt.Sprintf("could not open %s: %v", current, err)
			return chain, nil
		}

		exists, err := backup.CheckExistence()
		if err != nil {
			chain.Broken = true
			chain.BreakReason = DeltaPromotionChainUnreadable
			chain.BreakDetail = fmt.Sprintf("could not check whether %s exists: %v", current, err)
			return chain, nil
		}
		if !exists {
			chain.Broken = true
			chain.BreakReason = DeltaPromotionChainBroken
			chain.BreakDetail = fmt.Sprintf("%s is referenced by the chain but is not in storage", current)
			return chain, nil
		}

		sentinel, err := backup.GetSentinel()
		if err != nil {
			chain.Broken = true
			chain.BreakReason = DeltaPromotionChainUnreadable
			chain.BreakDetail = fmt.Sprintf("could not read the sentinel of %s: %v", current, err)
			return chain, nil
		}

		// The fields are read directly rather than through IsIncremental, which
		// panics on a sentinel that names an increment base without the rest of
		// the increment fields. Surviving a malformed sentinel is the point of
		// walking the chain, so it cannot be reached through something that dies
		// on one.
		if sentinel.IncrementFrom == nil {
			chain.FullBackupName = current
			chain.Depth = len(chain.Links) - 1
			return chain, nil
		}

		// A link that names an increment base without the rest of its increment
		// fields is inconsistent, and IsIncremental - which the push and fetch
		// paths both call - panics on exactly that. Treating it as a broken chain
		// promotes the next backup to full instead, which is both the safe answer
		// and the one that keeps the inconsistency from reaching code that dies
		// on it.
		if *sentinel.IncrementFrom == "" || sentinel.IncrementFromLSN == nil ||
			sentinel.IncrementFullName == nil || sentinel.IncrementCount == nil {
			chain.Broken = true
			chain.BreakReason = DeltaPromotionChainUnreadable
			chain.BreakDetail = fmt.Sprintf(
				"%s names an increment base but is missing the rest of its increment fields", current)
			return chain, nil
		}

		current = *sentinel.IncrementFrom
	}

	chain.Broken = true
	chain.BreakReason = DeltaPromotionChainCycle
	chain.BreakDetail = fmt.Sprintf("chain from %s is longer than %d links", backupName, maxChainWalk)

	return chain, nil
}

// DeltaDecision is the outcome of deciding whether the next backup may be a
// delta on top of a candidate base.
type DeltaDecision struct {
	// UseDelta is whether the next backup may be a delta.
	UseDelta bool
	// Reason is set when a delta was possible but is not being taken.
	Reason DeltaPromotionReason
	Detail string
	// Depth is the depth the next backup would have: 0 for a full backup, and
	// the base chain's depth plus one for a delta.
	Depth int
	// MaxDepth is the configured limit, from WALG_DELTA_MAX_STEPS.
	MaxDepth int
	// Chain is the resolved base chain, oldest first.
	Chain []string
}

// Promoted reports whether a delta was possible and was declined.
func (d DeltaDecision) Promoted() bool {
	return !d.UseDelta && d.Reason != ""
}

// LogDecision states the outcome once, in the terms an operator would ask about.
func (d DeltaDecision) LogDecision(baseName string) {
	if d.UseDelta {
		tracelog.InfoLogger.Printf(
			"Delta backup from %s: chain depth %d of %d.\n", baseName, d.Depth, d.MaxDepth)
		return
	}

	if d.Promoted() {
		tracelog.InfoLogger.Printf(
			"Doing full backup instead of a delta on %s: %s (%s).\n", baseName, d.Detail, d.Reason)
	}
}

// DeltaDecisionInput is what deciding delta-or-full depends on.
type DeltaDecisionInput struct {
	// Chain is the base backup's increment chain, walked from storage.
	Chain DeltaChain
	// RecordedCount is the base backup's own delta count, which is only used
	// under LATEST_FULL. See DecideDelta.
	RecordedCount *int
	// MaxDeltas is WALG_DELTA_MAX_STEPS.
	MaxDeltas int
	// FromFull is WALG_DELTA_ORIGIN=LATEST_FULL.
	FromFull bool
	// BaseIsPermanent and BaseHasStartLSN describe the candidate base backup.
	BaseIsPermanent bool
	BaseHasStartLSN bool
	// NextIsPermanent is whether the backup about to be taken is permanent.
	NextIsPermanent bool
}

// DecideDelta decides whether the next backup may be a delta on top of a
// candidate base, and promotes it to a full backup when it may not.
//
// What the limit counts depends on WALG_DELTA_ORIGIN, because the two modes
// build different shapes:
//
//   - LATEST (default) chains each delta onto the previous one, so the chain
//     deepens and a restore must apply every link. The depth walked from
//     storage is what the limit bounds.
//   - LATEST_FULL rebases every delta onto the full backup, so the chain is
//     never deeper than one link and a walked depth could never reach the
//     limit. There the limit bounds how many deltas accumulate on one full
//     backup - which is what keeps the full backup from getting arbitrarily
//     stale - and that is the base's own recorded count.
//
// Under LATEST_FULL the recorded count is therefore still trusted, but the walk
// is not wasted: a chain that is broken or unreadable promotes in either mode.
func DecideDelta(input DeltaDecisionInput) DeltaDecision {
	decision := DeltaDecision{MaxDepth: input.MaxDeltas, Chain: input.Chain.Links}

	if input.Chain.Broken {
		decision.Reason = input.Chain.BreakReason
		decision.Detail = input.Chain.BreakDetail
		return decision
	}

	nextDepth := input.Chain.Depth + 1
	if input.FromFull {
		nextDepth = 1
		if input.RecordedCount != nil {
			nextDepth = *input.RecordedCount + 1
		}
	}

	if nextDepth > input.MaxDeltas {
		decision.Reason = DeltaPromotionMaxDepth
		decision.Detail = fmt.Sprintf("this would be delta %d and the limit is %d",
			nextDepth, input.MaxDeltas)
		return decision
	}

	if !input.BaseHasStartLSN {
		decision.Reason = DeltaPromotionBaseWithoutLSN
		decision.Detail = "the base backup was made without delta support and records no start LSN"
		return decision
	}

	// A delta on a permanent backup pins that backup in place: deleting it would
	// strand the delta.
	if input.BaseIsPermanent && !input.NextIsPermanent && !input.FromFull {
		decision.Reason = DeltaPromotionBasePermanent
		decision.Detail = "the base backup is permanent"
		return decision
	}

	decision.UseDelta = true
	decision.Depth = nextDepth

	return decision
}

// warnOnDepthDisagreement reports a chain whose recorded count disagrees with
// the links actually in storage. The walked depth is the one that is acted on;
// this says so rather than silently correcting, because the disagreement means
// some earlier backup recorded a depth that was already wrong.
func warnOnDepthDisagreement(baseName string, recorded *int, walked int) {
	// A resolved chain only reaches here with a count on every delta link, so a
	// missing count means the base is a full backup, at depth zero by definition.
	if recorded == nil {
		return
	}

	if *recorded != walked {
		tracelog.WarningLogger.Printf(
			"%s records a delta count of %d but sits %d link(s) into a chain in storage. "+
				"Using the depth found in storage, so the limit is applied to the real chain.\n",
			baseName, *recorded, walked)
	}
}
