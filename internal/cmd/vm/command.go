// Package vm manages the router's virtual machines through the web management
// API (the local REST API does not expose them).
package vm

import (
	"fmt"

	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/ikuaidev/ikuai-cli/internal/webapi"
	"github.com/spf13/cobra"
)

// New returns the vm command group.
func New(app *cliapp.Runtime) *cobra.Command {
	group := &cobra.Command{
		Use:   "vm",
		Short: "Virtual machines (web management API)",
		Long: `List, start and stop the router's virtual machines.

Needs IKUAI_CLI_WEB_USER / IKUAI_CLI_WEB_PASSWORD: the local REST API has no VM endpoints.
Avoid the web UI's VM snapshots: on 4.0.308 they report success but none are kept, and the
VM pauses for several seconds.`,
	}
	group.AddCommand(listCmd(app), powerCmd(app, "start", "up", "Start a VM"), powerCmd(app, "stop", "down", "Stop a VM (powers it off)"))
	return group
}

func client(app *cliapp.Runtime) (*webapi.Client, error) {
	if err := app.RequireURL(); err != nil {
		return nil, err
	}
	c, err := webapi.FromEnv(app.Session.BaseURL)
	if err != nil {
		return nil, &cliapp.ValidationError{Message: err.Error()}
	}
	return c, nil
}

func machines(c *webapi.Client) ([]map[string]interface{}, error) {
	rows, err := c.List("qemu")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		switch fmt.Sprint(row["status"]) {
		case "1":
			row["state"] = "running"
		default:
			row["state"] = "stopped"
		}
	}
	return rows, nil
}

func listCmd(app *cliapp.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List virtual machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(app)
			if err != nil {
				return err
			}
			rows, err := machines(c)
			if err != nil {
				return err
			}
			app.DefaultColumns = []string{"id", "name", "state", "cpu_cores", "mem_size", "auto_start", "vnc_port"}
			app.PrintJSON(rows)
			return nil
		},
	}
}

func powerCmd(app *cliapp.Runtime, use, action, short string) *cobra.Command {
	c := &cobra.Command{
		Use:   use + " NAME",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(app)
			if err != nil {
				return err
			}
			rows, err := machines(c)
			if err != nil {
				return err
			}
			var target map[string]interface{}
			for _, row := range rows {
				if fmt.Sprint(row["name"]) == args[0] {
					target = row
				}
			}
			if target == nil {
				return &cliapp.ValidationError{Message: "no virtual machine named " + args[0]}
			}
			want := map[string]string{"up": "running", "down": "stopped"}[action]
			if target["state"] == want {
				_, _ = fmt.Fprintf(app.Stderr, "%s is already %s\n", args[0], want)
				return nil
			}
			yes, _ := cmd.Flags().GetBool("yes")
			if app.DryRun {
				app.PrintJSON(map[string]interface{}{"func_name": "qemu", "action": action, "param": map[string]interface{}{"id": target["id"]}})
				return nil
			}
			if action == "down" {
				if err := cliapp.Confirm(app.Stdout, "Power off "+args[0]+"?", yes); err != nil {
					return err
				}
			}
			if _, err := c.Call("qemu", action, map[string]interface{}{"id": target["id"]}); err != nil {
				return err
			}
			app.PrintJSON(map[string]interface{}{"name": args[0], "id": target["id"], "requested": want})
			return nil
		},
	}
	if action == "down" {
		c.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	}
	return c
}
