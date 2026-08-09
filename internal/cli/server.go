package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/server"
)

func newServerCommand() *cobra.Command {
	var (
		port           int
		responseFormat string
		requestIDKey   string
		keyStyle       string
		apiPrefix      string
		requireAuth    bool
		enableCORS     bool
		enableDocs     bool
	)
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start and manage the gpi API server",
	}
	start := &cobra.Command{
		Use:   "start",
		Short: "Start the gpi API server and job scheduler",
		Args:  cobra.NoArgs,
		RunE: withCtx(func(c *ctx, _ *cobra.Command, _ []string) error {
			srv := server.New(c.store, c.prov)
			if responseFormat != "" {
				srv.SetResponseEncoder(server.NewResponseEncoder(responseFormat))
			}
			if requestIDKey != "" {
				srv.SetRequestIDHeader(requestIDKey)
			}
			if keyStyle != "" {
				srv.SetKeyStyle(server.KeyStyle(keyStyle))
			}
			if apiPrefix != "" {
				srv.SetAPIPrefix(apiPrefix)
			}
			srv.AuthRequired = requireAuth
			srv.EnableCORS = enableCORS
			srv.EnableDocs = enableDocs
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			defer c.store.Close()
			return srv.Run(ctx, port)
		}),
	}
	start.Flags().IntVar(&port, "port", 8080, "HTTP listen port")
	start.Flags().StringVar(&responseFormat, "response-format", "", "API response format: raw|envelope (default: GPI_RESPONSE_FORMAT or raw)")
	start.Flags().StringVar(&requestIDKey, "request-id-header", "", "header key carrying the request id (default: GPI_REQUEST_ID_HEADER or x-request-id)")
	start.Flags().StringVar(&keyStyle, "api-key-style", "", "response key case style: camel|snake|pascal (default: GPI_API_RESPONSE_KEY_STYLE or camel)")
	start.Flags().StringVar(&apiPrefix, "api-prefix", "", "REST API URL prefix (default: GPI_API_PREFIX or /api/v1/gpi)")
	start.Flags().BoolVar(&requireAuth, "require-auth", false, "require a valid bearer service-account token on API requests")
	start.Flags().BoolVar(&enableCORS, "enable-cors", false, "enable permissive CORS headers")
	start.Flags().BoolVar(&enableDocs, "docs", false, "enable OpenAPI spec (/swagger.json) and Swagger/ReDoc UI (/docs, /redoc)")
	cmd.AddCommand(start, newServerTokenCommand())
	return cmd
}
