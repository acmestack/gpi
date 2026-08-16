package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	endpoint   = "https://compute.googleapis.com/compute/v1"
	tokenScope = "https://www.googleapis.com/auth/compute"
)

func init() {
	_ = os.MkdirAll(filepath.Join(homeDir(), ".config", "gpi"), 0o700)
}

// Credentials carries GCP service account or ADC credentials.
type Credentials struct {
	ProjectID         string
	JSONKeyPath       string // path to service account JSON key
	AccessToken       string // pre-obtained token (overrides JSON key)
	AccessTokenExpiry time.Time
}

// LoadCredentials loads GCP credentials from env vars or disk.
// Env vars: GCP_PROJECT_ID, GCP_SERVICE_ACCOUNT_KEY (path to JSON).
// Falls back to Application Default Credentials (gcloud).
func LoadCredentials() (Credentials, error) {
	creds := Credentials{
		ProjectID: os.Getenv("GCP_PROJECT_ID"),
	}
	keyPath := os.Getenv("GCP_SERVICE_ACCOUNT_KEY")
	if keyPath != "" {
		creds.JSONKeyPath = keyPath
	}
	if creds.ProjectID == "" {
		return creds, fmt.Errorf("GCP_PROJECT_ID not set")
	}
	return creds, nil
}

// Client is an HTTP client for the GCP Compute Engine API.
type Client struct {
	creds  Credentials
	region string
	http   *http.Client
	token  string
}

// NewClient returns a client using env/disk credentials.
func NewClient(region string) (*Client, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	return NewClientWithCreds(region, creds)
}

// NewClientWithCreds returns a client bound to explicit credentials.
func NewClientWithCreds(region string, creds Credentials) (*Client, error) {
	c := &Client{
		creds:  creds,
		region: region,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
	if err := c.refreshToken(); err != nil {
		return nil, fmt.Errorf("gcp auth: %w", err)
	}
	return c, nil
}

// refreshToken obtains an OAuth2 access token.
func (c *Client) refreshToken() error {
	if c.creds.AccessToken != "" && time.Now().Before(c.creds.AccessTokenExpiry) {
		c.token = c.creds.AccessToken
		return nil
	}
	if c.creds.JSONKeyPath != "" {
		return c.refreshFromServiceAccount()
	}
	return c.refreshFromADC()
}

// refreshFromServiceAccount uses gcloud to obtain a token from a service account key.
func (c *Client) refreshFromServiceAccount() error {
	out, err := exec.Command("gcloud", "auth", "activate-service-account",
		"--key-file="+c.creds.JSONKeyPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("activate service account: %s", strings.TrimSpace(string(out)))
	}
	return c.refreshFromADC()
}

// refreshFromADC uses gcloud application-default login.
func (c *Client) refreshFromADC() error {
	out, err := exec.Command("gcloud", "auth", "print-access-token",
		"--project="+c.creds.ProjectID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("get access token: %s", strings.TrimSpace(string(out)))
	}
	c.token = strings.TrimSpace(string(out))
	if c.token == "" {
		return fmt.Errorf("empty access token")
	}
	return nil
}

// APIError represents a GCP API error response.
type APIError struct {
	Code    int
	Message string
	Errors  []struct {
		Code    string
		Message string
	}
}

func (e *APIError) Error() string {
	if len(e.Errors) > 0 {
		return fmt.Sprintf("gcp api: %s (%s)", e.Errors[0].Message, e.Errors[0].Code)
	}
	return fmt.Sprintf("gcp api: %s (http %d)", e.Message, e.Code)
}

// call executes an authenticated GET request to the GCP Compute API.
func (c *Client) call(ctx context.Context, project, path string, out any) error {
	url := fmt.Sprintf("%s/projects/%s/%s", endpoint, project, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			var apiErr APIError
			if json.Unmarshal(body, &apiErr) == nil {
				apiErr.Code = resp.StatusCode
				lastErr = &apiErr
			} else {
				lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 200))
			}
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}
		if out != nil {
			return json.Unmarshal(body, out)
		}
		return nil
	}
	return lastErr
}

// callPost executes an authenticated POST request.
func (c *Client) callPost(ctx context.Context, project, path string, body any, out any) error {
	url := fmt.Sprintf("%s/projects/%s/%s", endpoint, project, path)
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil {
			apiErr.Code = resp.StatusCode
			return &apiErr
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// callDelete executes an authenticated DELETE request.
func (c *Client) callDelete(ctx context.Context, project, path string) error {
	url := fmt.Sprintf("%s/projects/%s/%s", endpoint, project, path)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
