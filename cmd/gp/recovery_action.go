// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package gp

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/greenplum"
)

const (
	recoveryActionDescription = "Update recovery.conf recovery_target_action"
)

var (
	actionCmd = &cobra.Command{
		Use: "recovery-action [promote|pause|shutdown]",

		Short: recoveryActionDescription,

		Args: cobra.ExactArgs(1),

		Run: func(cmd *cobra.Command, args []string) {
			logsDir := viper.GetString(conf.GPLogsDirectory)

			follower := greenplum.NewActionHandler(logsDir, restoreConfigPath)

			follower.UpdateAction(args[0])
		},
	}
)

func init() {
	actionCmd.Flags().StringVar(&restoreConfigPath, "restore-config", "", restoreConfigPathDescription)

	_ = actionCmd.MarkFlagRequired("restore-config")

	cmd.AddCommand(actionCmd)
}
