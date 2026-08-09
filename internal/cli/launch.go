package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/task"
)

func newLaunchCommand() *cobra.Command {
	var (
		clusterName   string
		cloudFlag     string
		region        string
		zone          string
		numNodes      int
		useSpot       bool
		optimizerName string
		dryRun        bool
		noConfirm     bool
	)
	cmd := &cobra.Command{
		Use:   "launch TASK.yaml",
		Short: "Launch a task on the cheapest feasible cloud/region",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			ts, err := task.Load(args[0])
			if err != nil {
				return err
			}
			if clusterName == "" {
				clusterName = ts.Name
			}
			if numNodes > 0 {
				ts.NumNodes = numNodes
			}

			opt, err := selectOptimizer(optimizerName)
			if err != nil {
				return err
			}

			var launch *optimizer.Launch
			if ts.Backend != task.BackendCloud {
				fmt.Printf("=== Backend: %s (no placement needed) ===\n", ts.Backend)
				launch = &optimizer.Launch{Cloud: ts.Backend, NumNodes: ts.NumNodes}
			} else {
				plan, err := opt.Optimize(cmd.Context(), &optimizer.Request{
					Resources: ts.Resources,
					Options: &optimizer.Options{
						NumNodes: ts.NumNodes,
						UseSpot:  useSpot,
						Cloud:    cloudFlag,
						Region:   region,
						Zone:     zone,
					},
				})
				if err != nil {
					return err
				}

				fmt.Printf("=== Optimizer summary (%s) ===\n", opt.Name())
				printPlan(plan)

				if dryRun {
					return nil
				}
				// Refresh the chosen instance's price right before launch so the
				// decision reflects the current market (spot fluctuates by the
				// minute). Failures fall back to the plan price.
				picked := plan.Launches[0]
				if live, err := refreshLaunchPrice(cmd.Context(), picked); err == nil && live != nil {
					fmt.Printf("Live price check %s/%s %s: on-demand $%.4f, spot $%.4f\n",
						picked.Cloud, picked.Region, picked.InstanceType, live.OnDemand, live.Spot)
					if live.Spot > 0 {
						picked.SpotCost = live.Spot
					}
					if live.OnDemand > 0 {
						picked.OnDemandCost = live.OnDemand
					}
					plan.TotalCostPerHour = picked.TotalCostPerHour()
					printPlan(plan)
				} else if err != nil {
					fmt.Printf("(live price refresh unavailable: %v)\n", err)
				}
				if !noConfirm {
					fmt.Printf("Launch cluster %q in %s/%s (%s)? [y/N] ", clusterName, plan.Launches[0].Cloud, plan.Launches[0].Region, plan.Launches[0].InstanceType)
					var ans string
					fmt.Scanln(&ans)
					if strings.ToLower(strings.TrimSpace(ans)) != "y" {
						fmt.Println("aborted")
						return nil
					}
				}
				launch = plan.Launches[0]
			}

			cluster, err := c.prov.Launch(cmd.Context(), clusterName, ts, launch)
			if err != nil {
				return err
			}
			fmt.Printf("Cluster %s provisioned: %d node(s) via %s\n",
				cluster.Name, cluster.NumNodes, cluster.Backend)

			code, err := c.prov.RunTask(cmd.Context(), clusterName, ts, func(line string) {
				fmt.Fprintf(os.Stdout, "(task) %s\n", line)
			})
			if err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("task run exited with code %d", code)
			}
			return nil
		}),
	}
	cmd.Flags().StringVarP(&clusterName, "cluster", "c", "", "cluster name (default: task name)")
	cmd.Flags().StringVar(&cloudFlag, "cloud", "", "cloud filter (comma-separated)")
	cmd.Flags().StringVarP(&region, "region", "r", "", "region filter")
	cmd.Flags().StringVar(&zone, "zone", "", "zone filter")
	cmd.Flags().IntVarP(&numNodes, "num-nodes", "n", 0, "number of nodes (overrides task)")
	cmd.Flags().BoolVar(&useSpot, "spot", false, "use spot instances")
	cmd.Flags().StringVar(&optimizerName, "optimizer", "", "placement optimizer or strategy: cost, time, or priority list like cost,time (default: cost)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only print the optimization plan")
	cmd.Flags().BoolVarP(&noConfirm, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newOptimizeCommand() *cobra.Command {
	var (
		cloudFlag     string
		region        string
		zone          string
		numNodes      int
		useSpot       bool
		optimizerName string
	)
	cmd := &cobra.Command{
		Use:   "optimize TASK.yaml",
		Short: "Show the placement plan for a task without launching",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(_ *ctx, cmd *cobra.Command, args []string) error {
			ts, err := task.Load(args[0])
			if err != nil {
				return err
			}
			if numNodes > 0 {
				ts.NumNodes = numNodes
			}
			opt, err := selectOptimizer(optimizerName)
			if err != nil {
				return err
			}
			plan, err := opt.Optimize(cmd.Context(), &optimizer.Request{
				Resources: ts.Resources,
				Options: &optimizer.Options{
					NumNodes: ts.NumNodes,
					UseSpot:  useSpot,
					Cloud:    cloudFlag,
					Region:   region,
					Zone:     zone,
				},
			})
			if err != nil {
				return err
			}
			printPlan(plan)
			return nil
		}),
	}
	cmd.Flags().StringVar(&cloudFlag, "cloud", "", "cloud filter (comma-separated)")
	cmd.Flags().StringVarP(&region, "region", "r", "", "region filter")
	cmd.Flags().StringVar(&zone, "zone", "", "zone filter")
	cmd.Flags().IntVarP(&numNodes, "num-nodes", "n", 0, "number of nodes")
	cmd.Flags().BoolVar(&useSpot, "spot", false, "use spot instances")
	cmd.Flags().StringVar(&optimizerName, "optimizer", "", "placement optimizer or strategy: cost, time, or priority list like cost,time (default: cost)")
	return cmd
}

// selectOptimizer resolves an optimizer or strategy by name via
// optimizer.Resolve, defaulting to the built-in cost optimizer when empty.
func selectOptimizer(name string) (optimizer.Optimizer, error) {
	return optimizer.Resolve(name)
}

func printPlan(plan *optimizer.Plan) {
	showTime := plan.TotalEstimatedTime > 0 || (len(plan.Launches) > 0 && plan.Launches[0].EstimatedTime > 0)
	if showTime {
		fmt.Printf("%-3s %-10s %-10s %-24s %-8s %-8s %-10s %-10s\n",
			"#", "CLOUD", "REGION", "INSTANCE", "NODES", "CPUS", "$/hr", "EST TIME")
		for _, l := range plan.Launches {
			fmt.Printf("%-3d %-10s %-10s %-24s %-8d %-8d %-10.3f %-10s\n",
				l.Order, l.Cloud, l.Region, l.InstanceType, l.NumNodes, l.VCPUs, l.CostPerHour(), formatHours(l.EstimatedTime))
		}
		fmt.Printf("\nTotal estimated time: %s\n", formatHours(plan.TotalEstimatedTime))
		return
	}
	fmt.Printf("%-3s %-10s %-10s %-24s %-8s %-8s %-10s\n",
		"#", "CLOUD", "REGION", "INSTANCE", "NODES", "CPUS", "$/hr")
	for _, l := range plan.Launches {
		fmt.Printf("%-3d %-10s %-10s %-24s %-8d %-8d %-10.3f\n",
			l.Order, l.Cloud, l.Region, l.InstanceType, l.NumNodes, l.VCPUs, l.CostPerHour())
	}
	fmt.Printf("\nTotal estimated cost: $%.3f/hour for %d node(s)\n",
		plan.TotalCostPerHour, plan.Launches[0].NumNodes)
}

// formatHours renders a duration in hours compactly (e.g. "0.042h", "1.5h").
func formatHours(h float64) string {
	if h <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.3fh", h)
}

// refreshLaunchPrice force-refreshes the live price for a single picked launch
// right before it is confirmed, bypassing the TTL cache. Returns nil when the
// cloud has no metadata source.
func refreshLaunchPrice(ctx context.Context, l *optimizer.Launch) (*catalog.Price, error) {
	if l == nil || !catalog.HasCloud(l.Cloud) {
		return nil, nil
	}
	prices, err := optimizer.PricesForced(ctx, l.Cloud, l.Region, []string{l.InstanceType})
	if err != nil {
		return nil, err
	}
	if prices == nil {
		return nil, nil
	}
	p, ok := prices[l.InstanceType]
	if !ok {
		return nil, nil
	}
	return &p, nil
}
