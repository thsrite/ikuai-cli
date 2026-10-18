// Package backup lists and restores objects saved before a change or delete.
package backup

import (
	"errors"
	"fmt"

	"github.com/ikuaidev/ikuai-cli/internal/api"
	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/spf13/cobra"
)

// New returns the backup command group.
func New(app *cliapp.Runtime) *cobra.Command {
	group := &cobra.Command{
		Use:   "backup",
		Short: "List or restore objects saved before a change or delete",
		Long: `Update, toggle and delete save the object first (to $IKUAI_CLI_BACKUP_DIR or
~/.ikuai-cli/backups). "backup restore FILE" writes it back: the object is replaced if it
still exists, or created again if it was deleted (the router assigns a new ID).`,
	}
	group.AddCommand(listCmd(app), restoreCmd(app))
	return group
}

func listCmd(app *cliapp.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List backups, newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			backups, err := cliapp.ListBackups()
			if err != nil {
				return err
			}
			app.DefaultColumns = []string{"saved_at", "api_path", "id", "file"}
			app.PrintJSON(backups)
			return nil
		},
	}
}

func restoreCmd(app *cliapp.Runtime) *cobra.Command {
	c := &cobra.Command{
		Use:   "restore FILE",
		Short: "Write a backed-up object back to the router",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.RequireAuth(); err != nil {
				return err
			}
			backup, err := cliapp.LoadBackup(args[0])
			if err != nil {
				return err
			}
			record, ok := cliapp.RecordFromGet(backup.Record)
			if !ok {
				return &cliapp.ValidationError{Message: "backup does not contain a router object"}
			}
			body := cliapp.InputBody(record)
			yes, _ := cmd.Flags().GetBool("yes")
			target := fmt.Sprintf("%s/%s", backup.APIPath, backup.ID)
			if err := cliapp.Confirm(app.Stdout, "Restore "+target+" from "+args[0]+"?", yes || app.APIClient.DryRun); err != nil {
				return err
			}
			collection := cliapp.APIBase + "/" + backup.APIPath
			reader := app.APIClient
			if reader.DryRun {
				reader = app.NewClient(app.Session.BaseURL, app.Session.Token)
			}
			current, err := reader.Get(collection+"/"+backup.ID, nil)
			var apiErr *api.APIError
			// Only a lookup the router answered (not auth or network) means "gone".
			if err != nil && (!errors.As(err, &apiErr) || apiErr.Code == 3007) {
				return err
			}
			_, found := cliapp.RecordFromGet(current)
			exists := err == nil && found
			if exists {
				raw, err := app.APIClient.Put(collection+"/"+backup.ID, body)
				if err != nil {
					return err
				}
				app.PrintRaw(raw)
				return nil
			}
			_, _ = fmt.Fprintf(app.Stderr, "%s no longer exists; creating it again\n", target)
			raw, err := app.APIClient.Post(collection, body)
			if err != nil {
				return err
			}
			app.PrintRaw(raw)
			return nil
		},
	}
	c.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	return c
}
