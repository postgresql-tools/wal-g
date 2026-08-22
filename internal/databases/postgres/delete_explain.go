// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

// ExplainedBackup is one backup as it appears in a delete plan.
type ExplainedBackup struct {
	Name       string    `json:"name"`
	FinishTime time.Time `json:"finish_time,omitempty"`
	Permanent  bool      `json:"permanent,omitempty"`
	// Restorable is whether the backup could serve a restore before the delete.
	// A backup that was already dead weight is worth distinguishing from one this
	// delete is about to kill.
	Restorable bool `json:"restorable"`
}

// DeleteExplanation is what a delete would do, in terms of what could still be
// recovered afterwards rather than in terms of object counts.
type DeleteExplanation struct {
	Command string `json:"command"`

	Objects     int    `json:"objects"`
	Bytes       int64  `json:"bytes"`
	BytesPretty string `json:"bytes_pretty"`

	BackupsDeleted  []ExplainedBackup `json:"backups_deleted"`
	BackupsRetained []ExplainedBackup `json:"backups_retained"`
	WalSegments     int               `json:"wal_segments_deleted"`

	RecoveryWindowBefore PITRWindowReport `json:"recovery_window_before"`
	RecoveryWindowAfter  PITRWindowReport `json:"recovery_window_after"`

	// Effects are the intended consequences: history given up, space reclaimed.
	Effects []string `json:"effects,omitempty"`
	// Warnings are consequences that are usually not intended. They do not stop
	// anything - --explain never deletes - but they are the reason to read the
	// report before adding --confirm.
	Warnings []string `json:"warnings,omitempty"`
}

// DeleteExplainer collects what a delete would remove and reports the recovery
// window on either side of it.
//
// The window before the delete is read when the explainer is built, so it
// reflects storage as it stands. The window after is computed by removing the
// planned objects from that same snapshot - not by re-listing storage, which
// would be describing a delete that has not happened.
type DeleteExplainer struct {
	command string
	before  PITRInputs
	plans   []internal.DeletePlan
	// loadErr defers a failure to read storage until the report is written, so a
	// delete plan is still reported even when the recovery window cannot be.
	loadErr error
}

// NewDeleteExplainer snapshots the current recovery window ahead of a delete.
func NewDeleteExplainer(rootFolder storage.Folder, command string) *DeleteExplainer {
	explainer := &DeleteExplainer{command: command}

	inputs, err := LoadPITRInputs(rootFolder)
	if err != nil {
		explainer.loadErr = err
		return explainer
	}

	explainer.before = inputs

	return explainer
}

// Collect receives a delete plan. It satisfies internal.CollectPlanFunc.
func (e *DeleteExplainer) Collect(plan internal.DeletePlan) error {
	e.plans = append(e.plans, plan)
	return nil
}

// Explain builds the report from everything collected so far.
func (e *DeleteExplainer) Explain() (DeleteExplanation, error) {
	explanation := DeleteExplanation{Command: e.command}

	var names []string
	for _, plan := range e.plans {
		explanation.Objects += len(plan.Objects)
		explanation.Bytes += plan.TotalSize()
		names = append(names, plan.Names()...)
	}
	explanation.BytesPretty = formatBytes(explanation.Bytes)

	if e.loadErr != nil {
		return explanation, e.loadErr
	}

	deletedBackups, deletedSegments := ClassifyStorageNames(names)
	explanation.WalSegments = len(deletedSegments)

	explanation.RecoveryWindowBefore = e.before.Compute()
	explanation.RecoveryWindowAfter = e.before.Without(names).Compute()

	restorableBefore := restorableBackupNames(&explanation.RecoveryWindowBefore, e.before.Backups)

	for i := range e.before.Backups {
		backup := &e.before.Backups[i]

		entry := ExplainedBackup{
			Name:       backup.Name,
			FinishTime: backup.FinishTime,
			Permanent:  backup.IsPermanent,
			Restorable: restorableBefore[backup.Name],
		}

		if deletedBackups[backup.Name] {
			explanation.BackupsDeleted = append(explanation.BackupsDeleted, entry)
		} else {
			explanation.BackupsRetained = append(explanation.BackupsRetained, entry)
		}
	}

	sortExplainedBackups(explanation.BackupsDeleted)
	sortExplainedBackups(explanation.BackupsRetained)

	explanation.Effects, explanation.Warnings = describeDelete(&explanation)

	return explanation, nil
}

// restorableBackupNames is the set of backups a window report found usable.
func restorableBackupNames(report *PITRWindowReport, backups []PITRBackup) map[string]bool {
	blocked := make(map[string]bool, len(report.Unrestorable))
	for _, unrestorable := range report.Unrestorable {
		blocked[unrestorable.Name] = true
	}

	restorable := make(map[string]bool, len(backups))
	for i := range backups {
		if name := backups[i].Name; !blocked[name] {
			restorable[name] = true
		}
	}

	return restorable
}

func sortExplainedBackups(backups []ExplainedBackup) {
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].FinishTime.Equal(backups[j].FinishTime) {
			return backups[i].Name < backups[j].Name
		}
		return backups[i].FinishTime.Before(backups[j].FinishTime)
	})
}

// describeDelete turns two window reports into the two things worth saying: what
// this delete is for, and what it costs that the operator may not have meant to
// pay.
func describeDelete(explanation *DeleteExplanation) (effects, warnings []string) {
	before, after := &explanation.RecoveryWindowBefore, &explanation.RecoveryWindowAfter

	if explanation.Objects == 0 {
		return []string{"Nothing matches: no objects would be deleted."}, nil
	}

	effects = append(effects, fmt.Sprintf("Reclaims %s across %d object(s): %d backup(s) and %d WAL segment(s).",
		explanation.BytesPretty, explanation.Objects,
		len(explanation.BackupsDeleted), explanation.WalSegments))

	switch {
	case before.Empty():
		// Nothing was recoverable to begin with, so there is no window to lose.
		effects = append(effects, "Nothing was restorable before this delete either.")

	case after.Empty():
		warnings = append(warnings,
			"This delete leaves NOTHING restorable: every backup that could serve a restore is removed, "+
				"or loses the WAL it needs.")

	default:
		if after.EarliestRestorableTime.After(before.EarliestRestorableTime) {
			effects = append(effects, fmt.Sprintf(
				"The earliest restorable point moves forward from %s to %s, giving up %s of history.",
				formatExplainTime(before.EarliestRestorableTime),
				formatExplainTime(after.EarliestRestorableTime),
				formatDuration(after.EarliestRestorableTime.Sub(before.EarliestRestorableTime))))
		}

		// Retention deletes trim the old end of the window. Losing the recent end
		// means recently archived WAL is going, which is almost never the intent.
		if after.LatestRestorableTime.Before(before.LatestRestorableTime) {
			warnings = append(warnings, fmt.Sprintf(
				"The latest restorable point moves BACK from %s to %s: this delete removes recent WAL.",
				formatExplainTime(before.LatestRestorableTime),
				formatExplainTime(after.LatestRestorableTime)))
		}

		effects = append(effects, fmt.Sprintf("Recoverable time goes from %s to %s.",
			windowCoverage(before), windowCoverage(after)))
	}

	if len(after.Gaps) > len(before.Gaps) {
		warnings = append(warnings, fmt.Sprintf(
			"This delete opens %d new gap(s) in the recovery window: the range between the remaining "+
				"backups is no longer recoverable end to end.", len(after.Gaps)-len(before.Gaps)))
	}

	// A backup left in storage that can no longer be restored is worse than a
	// deleted one: it still costs money and still shows up in backup-list.
	if stranded := strandedBackups(explanation); len(stranded) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d retained backup(s) become unrestorable but are NOT deleted, and will keep costing storage: %s.",
			len(stranded), joinNames(stranded)))
	}

	if permanent := permanentBackupsDeleted(explanation); len(permanent) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d permanent backup(s) are in scope: %s.", len(permanent), joinNames(permanent)))
	}

	return effects, warnings
}

// strandedBackups are backups this delete keeps but makes unrestorable, by
// removing WAL they need or a full backup they descend from.
func strandedBackups(explanation *DeleteExplanation) []string {
	blockedAfter := make(map[string]bool)
	for _, unrestorable := range explanation.RecoveryWindowAfter.Unrestorable {
		blockedAfter[unrestorable.Name] = true
	}

	var stranded []string
	for _, backup := range explanation.BackupsRetained {
		if backup.Restorable && blockedAfter[backup.Name] {
			stranded = append(stranded, backup.Name)
		}
	}

	return stranded
}

func permanentBackupsDeleted(explanation *DeleteExplanation) []string {
	var permanent []string
	for _, backup := range explanation.BackupsDeleted {
		if backup.Permanent {
			permanent = append(permanent, backup.Name)
		}
	}

	return permanent
}

func windowCoverage(report *PITRWindowReport) string {
	if report.Empty() {
		return "nothing"
	}
	return formatDuration(report.CoverageDuration())
}

func joinNames(names []string) string {
	const shown = 5
	if len(names) <= shown {
		return joinWithComma(names)
	}
	return fmt.Sprintf("%s and %d more", joinWithComma(names[:shown]), len(names)-shown)
}

func joinWithComma(names []string) string {
	result := ""
	for i, name := range names {
		if i > 0 {
			result += ", "
		}
		result += name
	}
	return result
}

func formatExplainTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// WriteExplanation renders the report. Text is for a person deciding whether to
// add --confirm; JSON is for a pipeline deciding the same thing.
func WriteExplanation(explanation *DeleteExplanation, format string, output io.Writer) error {
	return WriteReport(format, explanation, func(w io.Writer) { writeExplanationText(explanation, w) }, output)
}

func writeExplanationText(explanation *DeleteExplanation, output io.Writer) {
	fmt.Fprintf(output, "wal-g %s --explain\n\n", explanation.Command)

	fmt.Fprintf(output, "Would delete %d object(s), %s\n",
		explanation.Objects, explanation.BytesPretty)
	fmt.Fprintf(output, "  backups      %d deleted, %d retained\n",
		len(explanation.BackupsDeleted), len(explanation.BackupsRetained))
	fmt.Fprintf(output, "  WAL segments %d\n\n", explanation.WalSegments)

	if len(explanation.BackupsDeleted) > 0 {
		fmt.Fprintf(output, "Backups to delete\n")
		for _, backup := range explanation.BackupsDeleted {
			fmt.Fprintf(output, "  %s  %s%s\n", formatExplainTime(backup.FinishTime), backup.Name,
				explainBackupTags(backup))
		}
		fmt.Fprintln(output)
	}

	if len(explanation.BackupsRetained) > 0 {
		fmt.Fprintf(output, "Backups to keep\n")
		for _, backup := range explanation.BackupsRetained {
			fmt.Fprintf(output, "  %s  %s%s\n", formatExplainTime(backup.FinishTime), backup.Name,
				explainBackupTags(backup))
		}
		fmt.Fprintln(output)
	}

	fmt.Fprintf(output, "Recovery window\n")
	writeWindowLine(output, "  before ", &explanation.RecoveryWindowBefore)
	writeWindowLine(output, "  after  ", &explanation.RecoveryWindowAfter)
	fmt.Fprintln(output)

	if len(explanation.RecoveryWindowAfter.Unrestorable) > 0 {
		fmt.Fprintf(output, "Not restorable afterwards\n")
		for _, unrestorable := range explanation.RecoveryWindowAfter.Unrestorable {
			fmt.Fprintf(output, "  %s: %s\n", unrestorable.Name, unrestorable.Detail)
		}
		fmt.Fprintln(output)
	}

	for _, effect := range explanation.Effects {
		fmt.Fprintf(output, "       %s\n", effect)
	}

	for _, warning := range explanation.Warnings {
		fmt.Fprintf(output, "[WARN] %s\n", warning)
	}

	fmt.Fprintf(output, "\nNothing was deleted. Re-run with --confirm to execute.\n")
}

func explainBackupTags(backup ExplainedBackup) string {
	tags := ""
	if backup.Permanent {
		tags += "  [permanent]"
	}
	if !backup.Restorable {
		tags += "  [not restorable]"
	}
	return tags
}

func writeWindowLine(output io.Writer, label string, report *PITRWindowReport) {
	if report.Empty() {
		fmt.Fprintf(output, "%snothing restorable\n", label)
		return
	}

	fmt.Fprintf(output, "%s%s .. %s  (%s recoverable across %d window(s))\n",
		label,
		formatExplainTime(report.EarliestRestorableTime),
		formatExplainTime(report.LatestRestorableTime),
		formatDuration(report.CoverageDuration()),
		len(report.Windows))

	for _, gap := range report.Gaps {
		fmt.Fprintf(output, "%s  gap %s .. %s on timeline %d\n", label,
			formatExplainTime(gap.Start), formatExplainTime(gap.End), gap.Timeline)
	}
}

// ExplainOrLog writes the explanation, reporting a storage read failure without
// discarding the plan that was collected regardless.
func (e *DeleteExplainer) ExplainOrLog(format string, output io.Writer) error {
	explanation, err := e.Explain()
	if err != nil {
		tracelog.WarningLogger.Printf(
			"Could not determine the recovery window: %v. Reporting the delete plan only.\n", err)
	}

	return WriteExplanation(&explanation, format, output)
}
