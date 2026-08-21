// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pg

import (
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/internal/multistorage"
	"github.com/lateos-ai/wal-g/internal/multistorage/policies"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

const UseSentinelTimeFlag = "use-sentinel-time"

const UseSentinelTimeDescription = "Use backup creation time from sentinel for backups ordering."

const DeleteGarbageExamples = `  garbage           Deletes outdated WAL archives and leftover backups files from storage

  garbage ARCHIVES  Deletes only outdated WAL archives from storage

  garbage BACKUPS   Deletes only leftover backups files from storage`

const DeleteGarbageUse = "garbage [ARCHIVES|BACKUPS]"

const afterFlag = "after"

const explainFlag = "explain"

const explainDescription = "Report what the delete would remove and what could still be recovered afterwards, " +
	"without deleting anything."

const deleteFormatFlag = "format"

const deleteFormatDescription = "Output format for --explain: text or json. Default: text."

var confirmed = false

var deleteWithoutBackups = false

var useSentinelTime = false

var deleteTargetUserData = ""

var deleteExplain = false

var deleteFormat = "text"

// deleteCmd represents the delete command

var deleteCmd = &cobra.Command{
	Use: "delete",

	Short: internal.DeleteShortDescription, // TODO : improve description
}

var deleteBeforeCmd = &cobra.Command{
	Use: internal.DeleteBeforeUsageExample, // TODO : improve description

	Example: internal.DeleteBeforeExamples,

	Args: internal.DeleteBeforeArgsValidator,

	Run: runDeleteBefore,
}

var deleteRetainCmd = &cobra.Command{
	Use: internal.DeleteRetainUsageExample, // TODO : improve description

	Example: internal.DeleteRetainExamples,

	ValidArgs: internal.StringModifiers,

	Args: internal.DeleteRetainArgsValidator,

	Run: runDeleteRetain,
}

var deleteEverythingCmd = &cobra.Command{
	Use: internal.DeleteEverythingUsageExample, // TODO : improve description

	Example: internal.DeleteEverythingExamples,

	ValidArgs: internal.StringModifiersDeleteEverything,

	Args: internal.DeleteEverythingArgsValidator,

	Run: runDeleteEverything,
}

var deleteTargetCmd = &cobra.Command{
	Use: internal.DeleteTargetUsageExample, // TODO : improve description

	Example: internal.DeleteTargetExamples,

	Args: internal.DeleteTargetArgsValidator,

	Run: runDeleteTarget,
}

var deleteGarbageCmd = &cobra.Command{
	Use: DeleteGarbageUse,

	Example: DeleteGarbageExamples,

	Args: DeleteGarbageArgsValidator,

	Run: runDeleteGarbage,
}

// executeDelete builds the delete handler and runs one delete subcommand.
//
// In explain mode it wires a plan sink and writes the report afterwards, which
// is what keeps --explain identical in scope to the delete it describes: the
// same handler, the same arguments, the same filters, with deletion swapped for
// collection at the last step.
func executeDelete(
	description string,
	useSentinel bool,
	run func(handler *postgres.DeleteHandler, permanentBackups map[postgres.PermanentObject]bool, confirm bool),
) {
	folder := configureFolder()

	permanentBackups, permanentWals := postgres.GetPermanentBackupsAndWals(folder)

	var explainer *postgres.DeleteExplainer

	var options []internal.DeleteHandlerOption

	if deleteExplain {
		explainer = postgres.NewDeleteExplainer(folder, description)

		options = append(options, internal.CollectPlanFunc(explainer.Collect))
	}

	deleteHandler, err := postgres.NewDeleteHandler(

		folder, permanentBackups, permanentWals, useSentinel, options...)

	tracelog.ErrorLogger.FatalOnError(err)

	run(deleteHandler, permanentBackups, confirmed && !deleteExplain)

	if explainer != nil {
		tracelog.ErrorLogger.FatalOnError(explainer.ExplainOrLog(deleteFormat, os.Stdout))
	}
}

func runDeleteBefore(cmd *cobra.Command, args []string) {
	executeDelete("delete before "+strings.Join(args, " "), useSentinelTime,

		func(handler *postgres.DeleteHandler, _ map[postgres.PermanentObject]bool, confirm bool) {
			handler.HandleDeleteBefore(args, confirm)
		})
}

func runDeleteRetain(cmd *cobra.Command, args []string) {
	afterValue, _ := cmd.Flags().GetString(afterFlag)

	description := "delete retain " + strings.Join(args, " ")

	if afterValue != "" {
		description += " --after " + afterValue
	}

	executeDelete(description, useSentinelTime,

		func(handler *postgres.DeleteHandler, _ map[postgres.PermanentObject]bool, confirm bool) {
			if afterValue == "" {
				handler.HandleDeleteRetain(args, confirm)
			} else {
				handler.HandleDeleteRetainAfter(append(args, afterValue), confirm)
			}
		})
}

func runDeleteEverything(cmd *cobra.Command, args []string) {
	executeDelete("delete everything "+strings.Join(args, " "), useSentinelTime,

		func(handler *postgres.DeleteHandler,
			permanentBackups map[postgres.PermanentObject]bool, confirm bool) {
			permanentBackupNames := make([]string, 0, len(permanentBackups))

			for backup, isPerm := range permanentBackups {
				if isPerm {
					permanentBackupNames = append(permanentBackupNames, backup.Name)
				}
			}

			handler.HandleDeleteEverything(args, permanentBackupNames, confirm)
		})
}

func runDeleteTarget(cmd *cobra.Command, args []string) {
	findFullBackup := false

	modifier := internal.ExtractDeleteTargetModifierFromArgs(args)

	if modifier == internal.FindFullDeleteModifier {
		findFullBackup = true

		// remove the extracted modifier from args

		args = args[1:]
	}

	executeDelete("delete target "+strings.Join(args, " "), useSentinelTime,

		func(handler *postgres.DeleteHandler, _ map[postgres.PermanentObject]bool, confirm bool) {
			targetBackupSelector, err := internal.CreateTargetDeleteBackupSelector(

				cmd, args, deleteTargetUserData, postgres.NewGenericMetaFetcher())

			tracelog.ErrorLogger.FatalOnError(err)

			handler.HandleDeleteTarget(targetBackupSelector, confirm, findFullBackup)
		})
}

func runDeleteGarbage(cmd *cobra.Command, args []string) {
	executeDelete("delete garbage "+strings.Join(args, " "), false,

		func(handler *postgres.DeleteHandler, _ map[postgres.PermanentObject]bool, confirm bool) {
			tracelog.ErrorLogger.FatalOnError(

				handler.HandleDeleteGarbage(args, confirm, deleteWithoutBackups))
		})
}

func configureFolder() storage.Folder {
	multiSt, err := internal.ConfigureMultiStorage(true)

	tracelog.ErrorLogger.FatalfOnError("Failed to configure multi-storage: %v", err)

	rootFolder, err := multistorage.UseAllAliveStorages(multiSt.RootFolder())

	tracelog.InfoLogger.Printf("Backup to delete will be searched in storages: %v", multistorage.UsedStorages(rootFolder))

	tracelog.ErrorLogger.FatalOnError(err)

	return multistorage.SetPolicies(rootFolder, policies.UniteAllStorages)
}

func DeleteGarbageArgsValidator(cmd *cobra.Command, args []string) error {
	modifiers := []string{postgres.DeleteGarbageArchivesModifier, postgres.DeleteGarbageBackupsModifier}

	return internal.DeleteArgsValidator(args, modifiers, 0, 1)
}

func init() {
	Cmd.AddCommand(deleteCmd)

	deleteTargetCmd.Flags().StringVar(

		&deleteTargetUserData, internal.DeleteTargetUserDataFlag, "", internal.DeleteTargetUserDataDescription)

	deleteRetainCmd.Flags().StringP(afterFlag, "a", "", "Set the time after which retain backups")

	deleteGarbageCmd.Flags().BoolVar(&deleteWithoutBackups, "without-backup-check", false, "skip check for existing non-permanent backups")

	deleteCmd.AddCommand(deleteRetainCmd, deleteBeforeCmd, deleteEverythingCmd, deleteTargetCmd, deleteGarbageCmd)

	deleteCmd.PersistentFlags().BoolVar(&confirmed, internal.ConfirmFlag, false, "Confirms backup deletion")

	deleteCmd.PersistentFlags().BoolVar(&useSentinelTime, UseSentinelTimeFlag, false, UseSentinelTimeDescription)

	deleteCmd.PersistentFlags().BoolVar(&deleteExplain, explainFlag, false, explainDescription)

	deleteCmd.PersistentFlags().StringVar(&deleteFormat, deleteFormatFlag, "text", deleteFormatDescription)

	deleteCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Cobra runs only the nearest PersistentPreRun in the chain. Defining one
		// here shadows the root's, which is what asserts the storage settings and
		// applies the WAL/block size overrides, so call it through explicitly.
		Cmd.PersistentPreRun(cmd, args)

		// --explain and --confirm ask for opposite things. Silently letting one win
		// would mean either deleting when a preview was wanted, or not deleting when
		// a delete was wanted, and only one of those is recoverable.
		if deleteExplain && confirmed {
			return errors.New("--explain and --confirm cannot be combined: --explain never deletes anything")
		}

		return postgres.ValidateReportFormat(deleteFormat)
	}
}
