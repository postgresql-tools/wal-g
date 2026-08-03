// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package pg

import (
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	retentionValidateUse   = "retention-validate"
	retentionValidateShort = "Check that the retention policy delivers the recovery objectives you claim"
	retentionValidateLong  = `Validate a retention policy against the recovery objectives it is meant to
deliver.

Two questions are asked, and they fail independently:

  Does storage meet the objectives right now?
  Would it still meet them once the declared retention policy has been applied?

A storage that passes the first and fails the second is the case worth
catching: it looks healthy only because the policy has not caught up with it
yet. The second question is answered by running the declared policy through the
same delete handler "delete retain" uses, so what is validated is the delete
that would actually happen - not a model of it. Nothing is deleted.

Checks performed:
  rpo               how much data a failure right now would lose
  retention-window  whether the required period is continuously restorable
  policy-outcome    whether it still would be after the policy runs
  backup-cadence    whether the policy can keep meeting the window, at the
                    observed backup cadence, rather than only today

The exit code is 0 when nothing failed and 1 otherwise; warnings do not affect
it. Use --format json to consume the report from a monitoring or CI pipeline.

Durations accept a d suffix for days: 30d, 72h, 90m.`

	retentionRPOFlag        = "rpo"
	retentionRPODescription = "Most recent data loss tolerated, e.g. 1h. Default: WALG_RPO."

	retentionWindowFlag        = "retention-window"
	retentionWindowDescription = "How far back a restore must remain possible, e.g. 30d. " +
		"Default: WALG_RETENTION_WINDOW."

	retentionRetainFlag        = "retain"
	retentionRetainDescription = "Backup count the policy keeps, as passed to `delete retain`. " +
		"Default: WALG_RETENTION_COUNT."

	retentionFormatFlag        = "format"
	retentionFormatDescription = "Output format: text or json. Default: text."
)

var (
	retentionRPO      string
	retentionWindow   string
	retentionRetain   int
	retentionFormat   string
	retentionValidCmd = &cobra.Command{
		Use:   retentionValidateUse,
		Short: retentionValidateShort,
		Long:  retentionValidateLong,
		Args:  cobra.NoArgs,
		Run:   runRetentionValidate,
	}
)

func runRetentionValidate(cmd *cobra.Command, args []string) {
	objectives := postgres.RetentionObjectives{
		Format:      retentionFormat,
		RetainCount: resolveRetainCount(),
	}

	var err error

	objectives.RPO, err = resolveDurationObjective(retentionRPO, conf.RPOSetting)
	tracelog.ErrorLogger.FatalfOnError("Invalid --rpo: %v", err)

	objectives.RetentionWindow, err = resolveDurationObjective(retentionWindow, conf.RetentionWindowSetting)
	tracelog.ErrorLogger.FatalfOnError("Invalid --retention-window: %v", err)

	folder := configureFolder()

	// The policy is run through the same handler "delete retain" uses, with
	// deletion swapped for collection, so the window being validated is the one
	// the real delete would leave.
	explainer := postgres.NewDeleteExplainer(folder,
		"delete retain "+strconv.Itoa(objectives.RetainCount))

	if objectives.RetainCount > 0 {
		permanentBackups, permanentWals := postgres.GetPermanentBackupsAndWals(folder)

		deleteHandler, err := postgres.NewDeleteHandler(folder, permanentBackups, permanentWals,
			useSentinelTime, internal.CollectPlanFunc(explainer.Collect))
		tracelog.ErrorLogger.FatalOnError(err)

		deleteHandler.HandleDeleteRetain([]string{strconv.Itoa(objectives.RetainCount)}, false)
	}

	explanation, err := explainer.Explain()
	tracelog.ErrorLogger.FatalfOnError("Failed to determine the recovery window: %v", err)

	report := postgres.ValidateRetention(&explanation, objectives, utility.TimeNowCrossPlatformUTC())

	tracelog.ErrorLogger.FatalOnError(
		postgres.WriteRetentionReport(&report, objectives.Format, os.Stdout))

	if !report.Pass {
		os.Exit(1)
	}
}

// resolveRetainCount prefers the flag, then the setting, then no policy.
func resolveRetainCount() int {
	if retentionRetain > 0 {
		return retentionRetain
	}

	value, ok := conf.GetSetting(conf.RetentionCountSetting)
	if !ok || value == "" {
		return 0
	}

	count, err := strconv.Atoi(value)
	tracelog.ErrorLogger.FatalfOnError("Invalid "+conf.RetentionCountSetting+": %v", err)

	return count
}

func resolveDurationObjective(flagValue, setting string) (time.Duration, error) {
	if flagValue == "" {
		value, ok := conf.GetSetting(setting)
		if !ok || value == "" {
			return 0, nil
		}
		flagValue = value
	}

	return postgres.ParseObjectiveDuration(flagValue)
}

func init() {
	Cmd.AddCommand(retentionValidCmd)

	retentionValidCmd.Flags().StringVar(&retentionRPO, retentionRPOFlag, "", retentionRPODescription)
	retentionValidCmd.Flags().StringVar(&retentionWindow, retentionWindowFlag, "", retentionWindowDescription)
	retentionValidCmd.Flags().IntVar(&retentionRetain, retentionRetainFlag, 0, retentionRetainDescription)
	retentionValidCmd.Flags().StringVar(&retentionFormat, retentionFormatFlag, "text", retentionFormatDescription)
}
