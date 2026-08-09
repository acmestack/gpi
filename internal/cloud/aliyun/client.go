package aliyun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ecsVersion = "2014-05-26"

// Credentials holds Alibaba Cloud access keys.
type Credentials struct {
	AccessKeyID     string
	AccessKeySecret string
}

// Client talks to the ECS API. Each call signs the request with HMAC-SHA1.
type Client struct {
	cred     Credentials
	endpoint string
	region   string
	http     *http.Client
}

// LoadCredentials resolves credentials from env vars
// (ALIBABA_CLOUD_ACCESS_KEY_ID/SECRET) or ~/.aliyun/config.json.
func LoadCredentials() (Credentials, error) {
	ak := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	sk := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	if ak == "" || sk == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			cfg := filepath.Join(home, ".aliyun", "config.json")
			if data, err := os.ReadFile(cfg); err == nil {
				var parsed struct {
					AccessKeyID     string `json:"access_key_id"`
					AccessKeySecret string `json:"access_key_secret"`
				}
				if json.Unmarshal(data, &parsed) == nil {
					ak = parsed.AccessKeyID
					sk = parsed.AccessKeySecret
				}
			}
		}
	}
	if ak == "" || sk == "" {
		return Credentials{}, errors.New("aliyun credentials not found: set ALIBABA_CLOUD_ACCESS_KEY_ID/SECRET or configure ~/.aliyun/config.json")
	}
	return Credentials{AccessKeyID: ak, AccessKeySecret: sk}, nil
}

// NewClient builds a client with default (env/disk) credentials.
func NewClient(region string) (*Client, error) {
	cred, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	return NewClientWithCreds(region, cred)
}

// NewClientWithCreds builds a client with explicitly supplied credentials.
func NewClientWithCreds(region string, cred Credentials) (*Client, error) {
	endpoint := os.Getenv("ALIBABA_CLOUD_ENDPOINT")
	if endpoint == "" {
		endpoint = "ecs.aliyuncs.com"
	}
	return &Client{
		cred:     cred,
		endpoint: endpoint,
		region:   region,
		http:     &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// APIError represents a non-2xx response from the ECS API.
type APIError struct {
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aliyun api error [%s]: %s (http %d)", e.Code, e.Message, e.Status)
}

func (c *Client) call(ctx context.Context, action string, params map[string]string, out any) error {
	if params == nil {
		params = map[string]string{}
	}
	// Drop any Signature left over from a previous paginated call reusing the
	// same params map; it must never be part of the signed canonical string.
	delete(params, "Signature")
	params["Action"] = action
	params["Version"] = ecsVersion
	params["Format"] = "JSON"
	params["AccessKeyId"] = c.cred.AccessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = newNonce()
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if c.region != "" && action != "DescribeRegions" {
		params["RegionId"] = c.region
	}
	params["Signature"] = signRequest("POST", c.endpoint, c.cred.AccessKeySecret, params)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", "https://"+c.endpoint+"/", strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
			}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode >= 300 {
			var apiErr APIError
			msgPart := string(body)
			if json.Unmarshal(body, &apiErr) == nil {
				apiErr.Status = resp.StatusCode
				return &apiErr
			}
			_ = msgPart
			return fmt.Errorf("aliyun http %d: %s", resp.StatusCode, truncate(string(body), 500))
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("aliyun decode response: %w", err)
		}
		return nil
	}
	return lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func newNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
