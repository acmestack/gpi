package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
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
			fmt.Println(y.YAML)
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
				fmt.Printf("Cluster    : %s\n", h.ClusterName)
				fmt.Printf("Nodes      : %d\n", h.NumNodes)
				fmt.Printf("Cloud      : %s/%s (%s)\n", h.Cloud, h.Region, h.Zone)
				fmt.Printf("Instance   : %s\n", h.InstanceType)
				fmt.Printf("Backend    : %s\n", h.Backend)
				fmt.Printf("LaunchedAt : %s\n", formatTime(h.LaunchedAt))
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
				fmt.Printf("No events for cluster %s.\n", args[0])
				return nil
			}
			fmt.Printf("%-22s %-20s %-20s %-14s %s\n", "TIME", "FROM", "TO", "TYPE", "REQUEST ID")
			for _, e := range events {
				from := e.StartingStatus
				if from == "" {
					from = "-"
				}
				rid := e.RequestID
				if rid == "" {
					rid = "-"
				}
				fmt.Printf("%-22s %-20s %-20s %-14s %s\n",
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
			fmt.Printf("Cluster    : %s\n", cluster.Name)
			fmt.Printf("Status     : %s\n", cluster.Status)
			fmt.Printf("Cloud      : %s/%s\n", cluster.Cloud, cluster.Region)
			fmt.Printf("Topology   : %d head + %d worker (total %d)\n", headCount(cluster), workerCount(cluster), cluster.NumNodes)
			if cluster.NumNodes > 1 {
				if head != nil {
					fmt.Printf("Ray head   : %s:%s (dashboard http://%s:8265)\n", head.PrivateIP, "6379", head.PublicIP)
				}
			}
			if len(cluster.Labels) > 0 {
				fmt.Printf("Ray labels : %s\n", sortedKV(cluster.Labels))
			}
			if len(cluster.Tags) > 0 {
				fmt.Printf("Tags       : %s\n", sortedKV(cluster.Tags))
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
			fmt.Printf("%-16s %-8s %-18s %-18s %-12s %s\n",
				"NODE ID", "ROLE", "PUBLIC IP", "PRIVATE IP", "STATUS", "INSTANCE TYPE")
			for _, node := range cluster.Instances {
				role := node.Role
				if role == "" {
					role = "-"
				}
				fmt.Printf("%-16s %-8s %-18s %-18s %-12s %s\n",
					node.ID, role, node.PublicIP, node.PrivateIP, node.Status, node.InstanceType)
			}
			if showHealth {
				fmt.Println("\nLive health (gpilet):")
				for i := range cluster.Instances {
					node := &cluster.Instances[i]
					if node.PublicIP == "" {
						continue
					}
					line := c.prov.GpiletHealth(cmd.Context(), cluster, node)
					fmt.Printf("  %-16s %s\n", node.ID, line)
				}
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&showHealth, "health", false, "show live node health from gpilet")
	return cmd
}
