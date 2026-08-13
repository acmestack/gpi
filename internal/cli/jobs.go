package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/jobs"
)

func newJobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Submit and manage scheduled jobs",
	}
	cmd.AddCommand(
		newJobsSubmitCommand(),
		newJobsStatusCommand(),
		newJobsRunCommand(),
	)
	return cmd
}

func newJobsSubmitCommand() *cobra.Command {
	var (
		name          string
		schedule      string
		retries       int
		optimizerName string
		runNow        bool
	)
	cmd := &cobra.Command{
		Use:   "submit TASK.yaml",
		Short: "Register a job (with optional cron schedule)",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			mgr := jobs.New(c.store, c.prov)
			job, err := mgr.Submit(name, args[0], schedule, retries, optimizerName)
			if err != nil {
				return err
			}
			fmt.Printf("Job %s registered (schedule=%q, retries=%d).\n", job.Name, job.Schedule, job.Retries)
			if runNow {
				fmt.Printf("Running %s now...\n", job.Name)
				if err := mgr.RunNow(cmd.Context(), job.Name, func(line string) {
					fmt.Println("(job)", line)
				}); err != nil {
					return err
				}
				fmt.Printf("Job %s finished.\n", job.Name)
			}
			return nil
		}),
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "job name (default: task name)")
	cmd.Flags().StringVar(&schedule, "schedule", "", "cron schedule, e.g. \"0 0 * * *\" or \"@every 24h\" (empty = run on demand)")
	cmd.Flags().IntVar(&retries, "retries", 0, "number of retries on failure")
	cmd.Flags().StringVar(&optimizerName, "optimizer", "", "placement optimizer or strategy: cost, time, or priority list like cost,time (default: cost)")
	cmd.Flags().BoolVar(&runNow, "run", false, "run immediately after registering")
	return cmd
}

func newJobsStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show all jobs",
		Args:  cobra.NoArgs,
		RunE: withCtx(func(c *ctx, _ *cobra.Command, _ []string) error {
			jobList := c.store.ListJobs()
			if len(jobList) == 0 {
				fmt.Println("No jobs.")
				return nil
			}
			fmt.Printf("%-20s %-12s %-10s %-10s %-10s %-16s %s\n",
				"NAME", "STATUS", "RUNS", "FAILS", "SCHEDULE", "NEXT RUN", "LAST")
			for _, j := range jobList {
				next := formatTime(j.NextRun)
				last := formatTime(j.LastRun)
				fmt.Printf("%-20s %-12s %-10d %-10d %-10s %-16s %s\n",
					j.Name, j.Status, j.RunCount, j.FailCount, j.Schedule, next, last)
			}
			return nil
		}),
	}
}

func newJobsRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run JOB",
		Short: "Run a registered job immediately",
		Args:  cobra.ExactArgs(1),
		RunE: withCtx(func(c *ctx, cmd *cobra.Command, args []string) error {
			mgr := jobs.New(c.store, c.prov)
			if err := mgr.RunNow(cmd.Context(), args[0], func(line string) {
				fmt.Println("(job)", line)
			}); err != nil {
				return err
			}
			fmt.Printf("Job %s finished.\n", args[0])
			return nil
		}),
	}
}

func formatTime(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return timeFmt(ts)
}

func timeFmt(ts int64) string {
	return time.Unix(ts, 0).Format(time.RFC3339)
}
