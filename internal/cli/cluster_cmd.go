package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/logging"
)

func newClusterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Inspect Ray clusters (head/worker topology)",
	}
	cmd.AddCommand(
		newClusterStatusCommand(),
		newClusterNodesCommand(),
		newClusterYAMLCommand(),
		newClusterHistoryCommand(),
		newClusterEventsCommand(),
	)
	return cmd
}

func newClusterYAMLCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "yaml CLUSTER",
		Short: "Show the task YAML snapshot used to launch a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, _ *cobra.Command, args []string) error {
			y, err := c.store.GetClusterYAML(args[0])
			if err != nil {
				return err
			}
			logging.CLIPrintln(y.YAML)
			return nil
		}),
	}
}

func newClusterHistoryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "history CLUSTER",
		Short: "Show the recorded launch history of a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, _ *cobra.Command, args []string) error {
			for _, h := range c.store.ListClusterHistory() {
				if h.ClusterName != args[0] {
					continue
				}
				logging.CLIPrintf("Cluster    : %s\n", h.ClusterName)
				logging.CLIPrintf("Nodes      : %d\n", h.NumNodes)
				logging.CLIPrintf("Cloud      : %s/%s (%s)\n", h.Cloud, h.Region, h.Zone)
				logging.CLIPrintf("Instance   : %s\n", h.InstanceType)
				logging.CLIPrintf("Backend    : %s\n", h.Backend)
				logging.CLIPrintf("LaunchedAt : %s\n", formatTime(h.LaunchedAt))
				return nil
			}
			return fmt.Errorf("no history for cluster %s", args[0])
		}),
	}
}

func newClusterEventsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "events CLUSTER",
		Short: "Show the lifecycle events of a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, _ *cobra.Command, args []string) error {
			events := c.store.ListClusterEventsFor(args[0])
			if len(events) == 0 {
				logging.CLIPrintf("No events for cluster %s.\n", args[0])
				return nil
			}
			logging.CLIPrintf("%-22s %-20s %-20s %-14s %s\n", "TIME", "FROM", "TO", "TYPE", "REQUEST ID")
			for _, e := range events {
				from := e.StartingStatus
				if from == "" {
					from = "-"
				}
				rid := e.RequestID
				if rid == "" {
					rid = "-"
				}
				logging.CLIPrintf("%-22s %-20s %-20s %-14s %s\n",
					timeFmt(e.TransitionedAt), from, e.EndingStatus, e.Type, rid)
			}
			return nil
		}),
	}
}

func newClusterStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status CLUSTER",
		Short: "Show cluster role topology and Ray status",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			cluster, err := c.store.GetCluster(args[0])
			if err != nil {
				return err
			}
			head := cluster.Head()
			logging.CLIPrintf("Cluster    : %s\n", cluster.Name)
			logging.CLIPrintf("Status     : %s\n", cluster.Status)
			logging.CLIPrintf("Cloud      : %s/%s\n", cluster.Cloud, cluster.Region)
			logging.CLIPrintf("Topology   : %d head + %d worker (total %d)\n", headCount(cluster), workerCount(cluster), cluster.NumNodes)
			if cluster.NumNodes > 1 {
				if head != nil {
					logging.CLIPrintf("Ray head   : %s:%s (dashboard http://%s:8265)\n", head.PrivateIP, "6379", head.PublicIP)
				}
			}
			if len(cluster.Labels) > 0 {
				logging.CLIPrintf("Ray labels : %s\n", sortedKV(cluster.Labels))
			}
			if len(cluster.Tags) > 0 {
				logging.CLIPrintf("Tags       : %s\n", sortedKV(cluster.Tags))
			}
			return nil
		}),
	}
}

func sortedKV(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ", ")
}

func newClusterNodesCommand() *cobra.Command {
	var showHealth bool
	cmd := &cobra.Command{
		Use:   "nodes CLUSTER",
		Short: "List all nodes of a cluster with their roles and live health",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			cluster, err := c.store.GetCluster(args[0])
			if err != nil {
				return err
			}
			logging.CLIPrintf("%-16s %-8s %-18s %-18s %-12s %s\n",
				"NODE ID", "ROLE", "PUBLIC IP", "PRIVATE IP", "STATUS", "INSTANCE TYPE")
			for _, node := range cluster.Instances {
				role := node.Role
				if role == "" {
					role = "-"
				}
				logging.CLIPrintf("%-16s %-8s %-18s %-18s %-12s %s\n",
					node.ID, role, node.PublicIP, node.PrivateIP, node.Status, node.InstanceType)
			}
			if showHealth {
				logging.CLIPrintln("\nLive health (gpilet):")
				for i := range cluster.Instances {
					node := &cluster.Instances[i]
					if node.PublicIP == "" {
						continue
					}
					line := c.prov.GpiletHealth(cmd.Context(), cluster, node)
					logging.CLIPrintf("  %-16s %s\n", node.ID, line)
				}
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&showHealth, "health", false, "show live node health from gpilet")
	return cmd
}
