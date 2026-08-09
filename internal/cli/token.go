package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newServerTokenCommand adds token management subcommands. These talk to a
// running gpi server over HTTP; if the server requires auth, the caller passes
// an existing token via GPI_API_TOKEN.
func newServerTokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage service-account API tokens",
	}
	cmd.AddCommand(
		newTokenCreateCommand(),
		newTokenListCommand(),
		newTokenRevokeCommand(),
		newTokenRotateCommand(),
	)
	return cmd
}

func apiBase() string {
	if v := os.Getenv("GPI_API_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8080"
}

func apiClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func apiRequest(method, path string, body any, out any) error {
	req, err := http.NewRequest(method, apiBase()+path, nil)
	if err != nil {
		return err
	}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := os.Getenv("GPI_API_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := apiClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("api %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		// The server wraps array payloads as {"data":[...]} when a request_id
		// is injected; prefer unwrapping when possible so callers see the bare
		// array/object.
		unwrapped, err := tryUnwrapData(raw, out)
		if err != nil {
			return err
		}
		if unwrapped {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	return nil
}

// tryUnwrapData re-decodes raw into out via the "data" field when the top-level
// object is a wrapper and out is a slice; returns true if it unwrapped.
func tryUnwrapData(raw []byte, out any) (bool, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false, nil // not an object; fall through to direct unmarshal
	}
	data, ok := envelope["data"]
	if !ok {
		return false, nil
	}
	inner, err := json.Marshal(data)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(inner, out); err == nil {
		return true, nil
	}
	return false, nil // unwrap not applicable; caller falls back to whole body
}

func newTokenCreateCommand() *cobra.Command {
	var (
		name      string
		creator   string
		expiresIn int64
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new service-account API token",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			var resp map[string]any
			if err := apiRequest(http.MethodPost, "/api/v1/tokens", map[string]any{
				"name":       name,
				"creator":    creator,
				"expires_in": expiresIn,
			}, &resp); err != nil {
				return err
			}
			fmt.Printf("Token created (shown once):\n")
			fmt.Printf("  token_id   : %s\n", asString(resp["tokenId"]))
			fmt.Printf("  token_name : %s\n", asString(resp["tokenName"]))
			fmt.Printf("  token      : %s\n", asString(resp["token"]))
			if exp := toInt64(resp["expiresAt"]); exp > 0 {
				fmt.Printf("  expires_at : %s\n", time.Unix(exp, 0).Format(time.RFC3339))
			} else {
				fmt.Printf("  expires_at : never\n")
			}
			fmt.Printf("\nUse it as: Authorization: Bearer %s\n", asString(resp["token"]))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "token name")
	cmd.Flags().StringVar(&creator, "creator", "", "creator identifier")
	cmd.Flags().Int64Var(&expiresIn, "expires-in", 0, "validity in seconds (0 = never)")
	return cmd
}

func newTokenListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all service-account API tokens",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			var data []any
			if err := apiRequest(http.MethodGet, "/api/v1/tokens", nil, &data); err != nil {
				return err
			}
			if len(data) == 0 {
				fmt.Println("No tokens.")
				return nil
			}
			fmt.Printf("%-12s %-16s %-10s %-24s %-10s\n", "ID", "NAME", "CREATOR", "CREATED", "ACTIVE")
			for _, item := range data {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				id := asString(m["tokenId"])
				name := asString(m["tokenName"])
				creator := asString(m["creator"])
				created := toInt64(m["createdAt"])
				active := asBool(m["active"])
				fmt.Printf("%-12s %-16s %-10s %-24s %-10t\n",
					id, name, creator, formatTime(created), active)
			}
			return nil
		},
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

func asBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func newTokenRevokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke TOKEN_ID",
		Short: "Revoke (delete) a service-account API token",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := apiRequest(http.MethodDelete, "/api/v1/tokens/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Printf("Token %s revoked.\n", args[0])
			return nil
		},
	}
}

func newTokenRotateCommand() *cobra.Command {
	var expiresIn int64
	cmd := &cobra.Command{
		Use:   "rotate TOKEN_ID",
		Short: "Rotate a service-account API token (new secret)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var resp map[string]any
			if err := apiRequest(http.MethodPost, "/api/v1/tokens/"+args[0]+"/rotate", map[string]any{
				"expires_in": expiresIn,
			}, &resp); err != nil {
				return err
			}
			fmt.Printf("Token %s rotated. New secret (shown once):\n", asString(resp["tokenId"]))
			fmt.Printf("  token : %s\n", asString(resp["token"]))
			return nil
		},
	}
	cmd.Flags().Int64Var(&expiresIn, "expires-in", 0, "validity in seconds (0 = never)")
	return cmd
}
