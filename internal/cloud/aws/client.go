package aws

import (
	"context"
	"encoding/xml"
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

const ec2APIVersion = "2016-11-15"

// Credentials holds AWS access keys and an optional region.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
}

// LoadCredentials resolves credentials from env vars (AWS_ACCESS_KEY_ID /
// AWS_SECRET_ACCESS_KEY / AWS_REGION) or ~/.aws/credentials + ~/.aws/config.
func LoadCredentials() (Credentials, error) {
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if ak == "" || sk == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			credsFile := filepath.Join(home, ".aws", "credentials")
			if data, err := os.ReadFile(credsFile); err == nil {
				parsedAK, parsedSK := parseCredentialsFile(data, os.Getenv("AWS_PROFILE"))
				if parsedAK != "" && parsedSK != "" {
					ak, sk = parsedAK, parsedSK
				}
			}
			if region == "" {
				configFile := filepath.Join(home, ".aws", "config")
				if data, err := os.ReadFile(configFile); err == nil {
					region = parseConfigRegion(data, os.Getenv("AWS_PROFILE"))
				}
			}
		}
	}
	if ak == "" || sk == "" {
		return Credentials{}, errors.New("aws credentials not found: set AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY or configure ~/.aws/credentials")
	}
	if region == "" {
		region = "us-east-1"
	}
	return Credentials{AccessKeyID: ak, SecretAccessKey: sk, Region: region}, nil
}

func parseCredentialsFile(data []byte, profile string) (string, string) {
	if profile == "" {
		profile = "default"
	}
	var ak, sk string
	inProfile := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inProfile = strings.Trim(line, "[]") == profile
			continue
		}
		if !inProfile {
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok {
			val = strings.TrimSpace(val)
			switch strings.TrimSpace(key) {
			case "aws_access_key_id":
				ak = val
			case "aws_secret_access_key":
				sk = val
			}
		}
	}
	return ak, sk
}

func parseConfigRegion(data []byte, profile string) string {
	if profile == "" {
		profile = "default"
	}
	inProfile := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.Trim(line, "[]")
			name = strings.TrimPrefix(name, "profile ")
			inProfile = name == profile
			continue
		}
		if !inProfile {
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok && strings.TrimSpace(key) == "region" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// Client talks to the EC2 API. Each call is signed with SigV4.
type Client struct {
	cred   Credentials
	region string
	http   *http.Client
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
	if region == "" {
		region = cred.Region
	}
	if region == "" {
		region = "us-east-1"
	}
	return &Client{
		cred:   cred,
		region: region,
		http:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// APIError represents a non-2xx response from the EC2 API.
type APIError struct {
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aws api error [%s]: %s (http %d)", e.Code, e.Message, e.Status)
}

type apiErrorResponse struct {
	Errors struct {
		Error struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		} `xml:"Error"`
	} `xml:"Errors"`
}

func (c *Client) call(ctx context.Context, action string, params map[string]string, out any) error {
	if params == nil {
		params = map[string]string{}
	}
	params["Action"] = action
	params["Version"] = ec2APIVersion

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	payload := form.Encode()
	payloadHash := sha256hex(payload)

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := amzDate[:8]
	host := "ec2." + c.region + ".amazonaws.com"

	headers := map[string]string{
		"content-type":         "application/x-www-form-urlencoded; charset=utf-8",
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	auth := signV4("POST", host, "/", c.region, "ec2",
		c.cred.AccessKeyID, c.cred.SecretAccessKey, payloadHash, amzDate, dateStamp, headers)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", "https://"+host+"/", strings.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		req.Header.Set("X-Amz-Date", amzDate)
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
		req.Header.Set("Authorization", auth)

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
			var apiErr apiErrorResponse
			if xml.Unmarshal(body, &apiErr) == nil && apiErr.Errors.Error.Code != "" {
				return &APIError{Code: apiErr.Errors.Error.Code, Message: apiErr.Errors.Error.Message, Status: resp.StatusCode}
			}
			return fmt.Errorf("aws http %d: %s", resp.StatusCode, truncate(string(body), 500))
		}
		if out == nil {
			return nil
		}
		if err := xml.Unmarshal(body, out); err != nil {
			return fmt.Errorf("aws decode response: %w", err)
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
