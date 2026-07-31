// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package gp

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/greenplum"
	"github.com/lateos-ai/wal-g/internal/multistorage/policies"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	backupListShortDescription = "Prints available backups"

	PrettyFlag = "pretty"

	JSONFlag = "json"

	DetailFlag = "detail"
)

var (

	// backupListCmd represents the backupList command

	backupListCmd = &cobra.Command{
		Use: "backup-list",

		Short: backupListShortDescription, // TODO : improve description

		Args: cobra.NoArgs,

		Run: func(cmd *cobra.Command, args []string) {
			rootFolder, err := getMultistorageRootFolder(false, policies.UniteAllStorages)

			tracelog.ErrorLogger.FatalOnError(err)

			if detail {
				greenplum.HandleDetailedBackupList(rootFolder, pretty, jsonOutput)
			} else {
				internal.HandleDefaultBackupList(rootFolder.GetSubFolder(utility.BaseBackupPath), pretty, jsonOutput)
			}
		},
	}

	pretty = false

	jsonOutput = false

	detail = false
)

func init() {
	cmd.AddCommand(backupListCmd)

	// TODO: Merge similar backup-list functionality

	// to avoid code duplication in command handlers

	backupListCmd.Flags().BoolVar(&pretty, PrettyFlag, false, "Prints more readable output")

	backupListCmd.Flags().BoolVar(&jsonOutput, JSONFlag, false, "Prints output in json format")

	backupListCmd.Flags().BoolVar(&detail, DetailFlag, false, "Prints extra backup details")

	backupListCmd.Flags().StringVar(&targetStorage, "target-storage", "",

		targetStorageDescription)
}
