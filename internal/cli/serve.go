package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/serve"
	"github.com/acmestack/gpi/internal/task"
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Deploy and manage replicated services",
	}
	cmd.AddCommand(
		newServeUpCommand(),
		newServeStatusCommand(),
		newServeDownCommand(),
	)
	return cmd
}

func newServeUpCommand() *cobra.Command {
	var (
		name      string
		cloudFlag string
		region    string
		noConfirm bool
	)
	cmd := &cobra.Command{
		Use:   "up SERVICE.yaml",
		Short: "Deploy a service with multiple replicas across clouds/regions",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			ts, err := task.Load(args[0])
			if err != nil {
				return err
			}
			if ts.Service == nil {
				return fmt.Errorf("task has no service section; add a service block to deploy it as a service")
			}
			if name == "" {
				name = ts.Name
			}
			plan, err := optimizer.Default().Optimize(cmd.Context(), &optimizer.Request{
				Resources: ts.Resources,
				Options: &optimizer.Options{
					Cloud:  cloudFlag,
					Region: region,
				},
			})
			if err != nil {
				return err
			}
			if !noConfirm {
				fmt.Printf("Deploy service %q with %d replica(s), port %d, base instance %s in %s/%s? [y/N] ",
					name, ts.Service.Replicas, ts.Service.Port, plan.Launches[0].InstanceType, plan.Launches[0].Cloud, plan.Launches[0].Region)
				var ans string
				fmt.Scanln(&ans)
				if ans != "y" && ans != "Y" {
					fmt.Println("aborted")
					return nil
				}
			}
			mgr := serve.New(c.store, c.prov)
			svc, err := mgr.Up(cmd.Context(), name, ts, plan)
			if err != nil {
				return err
			}
			fmt.Printf("Service %s is running.\n", svc.Name)
			for _, ep := range svc.Endpoints {
				fmt.Printf("  endpoint: http://%s\n", ep)
			}
			return nil
		}),
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "service name (default: task name)")
	cmd.Flags().StringVar(&cloudFlag, "cloud", "", "cloud filter")
	cmd.Flags().StringVarP(&region, "region", "r", "", "region filter")
	cmd.Flags().BoolVarP(&noConfirm, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newServeStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show all deployed services",
		Args:  cobra.NoArgs,
		RunE: withCtx(func(c *ctx, _ *cobra.Command, _ []string) error {
			services := c.store.ListServices()
			if len(services) == 0 {
				fmt.Println("No services.")
				return nil
			}
			fmt.Printf("%-20s %-10s %-6s %-10s %s\n", "NAME", "STATUS", "REPL", "PORT", "ENDPOINTS")
			for _, svc := range services {
				endpoints := ""
				for i, ep := range svc.Endpoints {
					if i > 0 {
						endpoints += ","
					}
					endpoints += ep
				}
				fmt.Printf("%-20s %-10s %-6d %-10d %s\n", svc.Name, svc.Status, svc.Replicas, svc.Port, endpoints)
			}
			return nil
		}),
	}
}

func newServeDownCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "down SERVICE",
		Short: "Tear down a service and all its replicas",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			svc, err := c.store.GetService(args[0])
			if err != nil {
				return err
			}
			for _, clusterName := range svc.ReplicaClusters {
				if err := c.prov.Down(cmd.Context(), clusterName); err != nil {
					fmt.Fprintln(os.Stderr, "warn:", err)
				}
			}
			if err := c.store.DeleteService(args[0]); err != nil {
				return err
			}
			fmt.Printf("Service %s torn down.\n", args[0])
			return nil
		}),
	}
}
