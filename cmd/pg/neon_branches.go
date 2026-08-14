// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package pg

import (
	"context"
	// Aliased: package pg already has a bool named json (see backup_list.go).
	encjson "encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wal-g/tracelog"

	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/pkg/neon"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	neonBranchesUse   = "neon-branches"
	neonBranchesShort = "List the Neon branches wal-g created"
	neonBranchesLong  = `List the Neon branches left behind by neon-drill.

A drill deletes its branch on every exit path, but a host that was powered off
mid-drill cannot. Every branch listed here is a running compute endpoint that is
still being paid for, so an empty list is the expected steady state.

Only branches named with the walg-drill- prefix are listed. Branches you created
yourself are never shown and never touched.

Credentials come from WALG_NEON_API_KEY and WALG_NEON_PROJECT_ID. This command
does not read backup storage, so it works without any WALG_*_PREFIX configured.`

	neonBranchesProjectFlag        = "neon-project"
	neonBranchesProjectDescription = "Neon project to list. Default: WALG_NEON_PROJECT_ID."
)

var (
	neonBranchesProject string
	neonBranchesFormat  string

	neonBranchesCmd = &cobra.Command{
		Use:   neonBranchesUse,
		Short: neonBranchesShort,
		Long:  neonBranchesLong,
		Args:  cobra.NoArgs,
		// This command talks only to the Neon control plane. Without this the
		// root PersistentPreRun would demand a configured storage prefix.
		Annotations: map[string]string{"NoStorage": ""},
		Run:         runNeonBranches,
	}
)

func runNeonBranches(_ *cobra.Command, _ []string) {
	projectID := neonBranchesProject
	if projectID == "" {
		projectID = viper.GetString(conf.NeonProjectIDSetting)
	}

	client, err := neon.NewClient(neon.Config{
		APIKey:    viper.GetString(conf.NeonAPIKeySetting),
		ProjectID: projectID,
		Endpoint:  viper.GetString(conf.NeonAPIEndpointSetting),
	})
	tracelog.ErrorLogger.FatalOnError(err)

	branches, err := client.ListDrillBranches(context.Background())
	tracelog.ErrorLogger.FatalOnError(err)

	if neonBranchesFormat == "json" {
		data, err := encjson.MarshalIndent(branches, "", "  ")
		tracelog.ErrorLogger.FatalOnError(err)

		fmt.Fprintln(os.Stdout, string(data))

		return
	}

	writeNeonBranchesText(branches)
}

func writeNeonBranchesText(branches []neon.Branch) {
	if len(branches) == 0 {
		fmt.Fprintln(os.Stdout, "No wal-g drill branches. Nothing is being paid for.")

		return
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(writer, "NAME\tID\tCREATED\tAGE")

	now := utility.TimeNowCrossPlatformUTC()

	for _, branch := range branches {
		created := "-"
		age := "-"

		if !branch.CreatedAt.IsZero() {
			created = branch.CreatedAt.UTC().Format(time.RFC3339)
			age = now.Sub(branch.CreatedAt).Round(time.Minute).String()
		}

		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", branch.Name, branch.ID, created, age)
	}

	_ = writer.Flush()

	fmt.Fprintf(os.Stdout, "\n%d drill branch(es) still present. Each one is a billable compute endpoint.\n",
		len(branches))
}

func init() {
	Cmd.AddCommand(neonBranchesCmd)

	neonBranchesCmd.Flags().StringVar(&neonBranchesProject, neonBranchesProjectFlag, "",
		neonBranchesProjectDescription)
	neonBranchesCmd.Flags().StringVar(&neonBranchesFormat, formatFlag, "text", formatDescription)
}
