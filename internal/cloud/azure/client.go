package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	tokenEndpoint = "https://login.microsoftonline.com/%s/oauth2/token"
	resourceID    = "https://management.azure.com/"
)

func init() {
	_ = os.MkdirAll(filepath.Join(homeDir(), ".config", "gpi"), 0o700)
}

// Credentials carries Azure service principal or default credentials.
type Credentials struct {
	SubscriptionID string
	TenantID       string
	ClientID       string
	ClientSecret   string
	AccessToken    string
	TokenExpiry    time.Time
}

// LoadCredentials loads Azure credentials from env vars.
// Env vars: AZURE_SUBSCRIPTION_ID, AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_CLIENT_SECRET.
func LoadCredentials() (Credentials, error) {
	creds := Credentials{
		SubscriptionID: os.Getenv("AZURE_SUBSCRIPTION_ID"),
		TenantID:       os.Getenv("AZURE_TENANT_ID"),
		ClientID:       os.Getenv("AZURE_CLIENT_ID"),
		ClientSecret:   os.Getenv("AZURE_CLIENT_SECRET"),
	}
	if creds.SubscriptionID == "" {
		return creds, fmt.Errorf("AZURE_SUBSCRIPTION_ID not set")
	}
	return creds, nil
}

// Client is an HTTP client for the Azure Resource Manager API.
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
		return nil, fmt.Errorf("azure auth: %w", err)
	}
	return c, nil
}

// refreshToken obtains an OAuth2 token using client credentials.
func (c *Client) refreshToken() error {
	if c.creds.AccessToken != "" && time.Now().Before(c.creds.TokenExpiry) {
		return nil
	}
	if c.creds.TenantID == "" || c.creds.ClientID == "" || c.creds.ClientSecret == "" {
		return fmt.Errorf("missing azure credentials (set AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_CLIENT_SECRET)")
	}

	url := fmt.Sprintf(tokenEndpoint, c.creds.TenantID)
	data := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s&resource=%s",
		c.creds.ClientID, c.creds.ClientSecret, resourceID)
	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("token error: http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}
	c.creds.AccessToken = tokenResp.AccessToken
	c.token = tokenResp.AccessToken

	// Parse expires_in (seconds) and add a 5-min buffer
	if seconds, err := time.ParseDuration(tokenResp.ExpiresIn + "s"); err == nil {
		c.creds.TokenExpiry = time.Now().Add(seconds - 5*time.Minute)
	} else {
		c.creds.TokenExpiry = time.Now().Add(55 * time.Minute)
	}
	return nil
}

// APIError represents an Azure API error.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("azure api: %s (%s)", e.Message, e.Code)
}

// call executes an authenticated GET request to Azure Resource Manager.
func (c *Client) call(ctx context.Context, path string, out any) error {
	url := fmt.Sprintf("https://management.azure.com%s?api-version=2024-03-01", path)
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
func (c *Client) callPost(ctx context.Context, path string, body any, out any) error {
	url := fmt.Sprintf("https://management.azure.com%s?api-version=2024-03-01", path)
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
func (c *Client) callDelete(ctx context.Context, path string) error {
	url := fmt.Sprintf("https://management.azure.com%s?api-version=2024-03-01", path)
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
