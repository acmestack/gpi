package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/logging"
	"github.com/acmestack/gpi/internal/state"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show all clusters and their states",
		Args:  cobra.NoArgs,
		RunE: withCtx(func(c *ctx, _ *cobra.Command, _ []string) error {
			clusters := c.store.ListClusters()
			if len(clusters) == 0 {
				logging.CLIPrintln("No clusters.")
				return nil
			}
			logging.CLIPrintf("%-20s %-12s %-10s %-12s %-6s %-10s %-24s %-16s\n",
				"NAME", "STATUS", "CLOUD", "REGION", "NODES", "ROLE", "INSTANCE", "PUBLIC IP")
			for _, cl := range clusters {
				inst := ""
				if cl.Launch != nil {
					inst = cl.Launch.InstanceType
				}
				logging.CLIPrintf("%-20s %-12s %-10s %-12s %-6d %-10s %-24s %-16s\n",
					cl.Name, cl.Status, cl.Cloud, cl.Region, cl.NumNodes, clusterRoles(cl), inst, cl.GetNodeIP())
			}
			return nil
		}),
	}
}

func clusterRoles(cl *state.Cluster) string {
	if cl.NumNodes <= 1 {
		return "-"
	}
	return fmt.Sprintf("%dhead/%dworker", headCount(cl), workerCount(cl))
}

func headCount(cl *state.Cluster) int {
	n := 0
	for i := range cl.Instances {
		if cl.Instances[i].Role == state.RoleHead {
			n++
		}
	}
	return n
}

func workerCount(cl *state.Cluster) int {
	n := 0
	for i := range cl.Instances {
		if cl.Instances[i].Role == state.RoleWorker {
			n++
		}
	}
	return n
}

func newDownCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "down CLUSTER",
		Short: "Terminate a cluster and delete its instances",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			if err := c.prov.Down(cmd.Context(), args[0]); err != nil {
				return err
			}
			logging.CLIPrintf("Cluster %s terminated.\n", args[0])
			return nil
		}),
	}
}

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop CLUSTER",
		Short: "Stop a cluster (instances stopped, state kept)",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			if err := c.prov.Stop(cmd.Context(), args[0]); err != nil {
				return err
			}
			logging.CLIPrintf("Cluster %s stopped.\n", args[0])
			return nil
		}),
	}
}

func newStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start CLUSTER",
		Short: "Start a stopped cluster",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			if err := c.prov.Start(cmd.Context(), args[0]); err != nil {
				return err
			}
			logging.CLIPrintf("Cluster %s started.\n", args[0])
			return nil
		}),
	}
}

func newExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exec CLUSTER -- CMD",
		Short: "Execute a command on a cluster's head node",
		Args:  cobra.MinimumNArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			cluster, command := args[0], args[1:]
			script := joinCommand(command)
			code, err := c.prov.Exec(cmd.Context(), cluster, script, func(line string) {
				// Streamed exec output: interactive UX, not a log.
				logging.CLIPrintln("(exec)", line)
			})
			if err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("command exited with code %d", code)
			}
			return nil
		}),
	}
}

func joinCommand(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += shellQuote(p)
	}
	return out
}

func shellQuote(s string) string {
	needsQuote := false
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '=' || r == ',' || r == '+') {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	return "'" + s + "'"
}
