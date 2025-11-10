package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	
	"github.com/argoproj/argo-workflows/v3/util/logging"
	"github.com/argoproj/argo-workflows/v3/workflow/executor/local"
)

// NewLocalCommand returns a new instance of the `argo local` command
func NewLocalCommand() *cobra.Command {
	var (
		port int
	)

	command := &cobra.Command{
		Use:   "local",
		Short: "Start a local workflow execution server",
		Long: `Start a local workflow execution server that runs workflows using Docker containers instead of Kubernetes.

This allows you to test and develop workflows locally without needing a Kubernetes cluster.
Workflows are submitted to the server via HTTP API and executed as ephemeral Docker containers.

Example:
  # Start the local server on default port 8080
  argo local

  # Start the local server on a custom port
  argo local --port 9090

  # Submit a workflow to the local server
  curl -X POST http://localhost:8080/api/v1/workflows \
    -H "Content-Type: application/yaml" \
    --data-binary @my-workflow.yaml

  # List workflows
  curl http://localhost:8080/api/v1/workflows

  # Get workflow status
  curl http://localhost:8080/api/v1/workflows/my-workflow
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := logging.RequireLoggerFromContext(ctx)

			logger.WithField("port", port).Info(ctx, "Starting Argo Workflows local execution server")

			// Create server
			server, err := local.NewServer(ctx, port)
			if err != nil {
				return fmt.Errorf("failed to create server: %w", err)
			}
			defer server.Close()

			// Set up signal handling
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			// Create cancellable context
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			// Start server in goroutine
			errChan := make(chan error, 1)
			go func() {
				if err := server.Start(ctx); err != nil {
					errChan <- err
				}
			}()

			logger.Info(ctx, "Local execution server started successfully")
			logger.WithField("url", fmt.Sprintf("http://localhost:%d", port)).Info(ctx, "Server is ready to accept workflows")

			// Wait for signal or error
			select {
			case <-sigChan:
				logger.Info(ctx, "Received shutdown signal")
				cancel()
			case err := <-errChan:
				logger.WithError(err).Error(ctx, "Server error")
				return err
			}

			logger.Info(ctx, "Server shutdown complete")
			return nil
		},
	}

	command.Flags().IntVar(&port, "port", 8080, "Port to listen on")

	return command
}
