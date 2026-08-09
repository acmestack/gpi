package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// spotPriceHistoryItem is one entry of DescribeSpotPriceHistory.
type spotPriceHistoryItem struct {
	InstanceType     string `xml:"instanceType"`
	SpotPrice        string `xml:"spotPrice"`
	Timestamp        string `xml:"timestamp"`
	AvailabilityZone string `xml:"availabilityZone"`
}

type spotPriceHistoryResp struct {
	SpotPriceHistorySet struct {
		Item []spotPriceHistoryItem `xml:"item"`
	} `xml:"spotPriceHistorySet"`
}

// DescribeSpotPriceHistory returns the most recent spot price for each given
// instance type in region. Only Linux/UNIX on-demand-product instances are
// considered; the API returns newest entries first, so the first occurrence of
// each type wins. Types with no history are omitted.
func (c *Client) DescribeSpotPriceHistory(ctx context.Context, region string, instanceTypes []string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, batch := range batchStrings(instanceTypes, 10) {
		params := map[string]string{
			"ProductDescription": "Linux/UNIX",
			"MaxResults":         "100",
		}
		for i, it := range batch {
			params[fmt.Sprintf("InstanceType.%d", i+1)] = it
		}
		// Limit the lookback so the response stays small but recent enough to
		// carry a current price (spot history keeps hours of entries).
		params["StartTime"] = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
		var resp spotPriceHistoryResp
		if err := c.call(ctx, "DescribeSpotPriceHistory", params, &resp); err != nil {
			return out, err
		}
		for _, item := range resp.SpotPriceHistorySet.Item {
			if _, seen := out[item.InstanceType]; seen {
				continue
			}
			p, err := parsePrice(item.SpotPrice)
			if err != nil || p <= 0 {
				continue
			}
			out[item.InstanceType] = p
		}
	}
	return out, nil
}

func batchStrings(items []string, size int) [][]string {
	if len(items) == 0 {
		return nil
	}
	var batches [][]string
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

func parsePrice(s string) (float64, error) {
	var p float64
	if _, err := fmt.Sscanf(s, "%f", &p); err != nil {
		return 0, err
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// AWS Pricing API (on-demand). Endpoint api.pricing.<region>.amazonaws.com,
// JSON protocol (application/x-amz-json-1.1). It is a different service from
// EC2, so it signs with service "pricing" but reuses the SigV4 code.

// pricingClient performs GetProducts calls against the AWS Pricing API.
type pricingClient struct {
	cred   Credentials
	region string
	http   *http.Client
}

func newPricingClient(cred Credentials, region string) *pricingClient {
	return &pricingClient{cred: cred, region: region, http: &http.Client{Timeout: 60 * time.Second}}
}

type getProductsRequest struct {
	ServiceCode string      `json:"ServiceCode"`
	Filters     []priceTerm `json:"Filters"`
	MaxResults  int         `json:"MaxResults,omitempty"`
	NextToken   string      `json:"NextToken,omitempty"`
}

type priceTerm struct {
	Type  string `json:"Type"`
	Field string `json:"Field"`
	Value string `json:"Value"`
}

type getProductsResponse struct {
	PriceList []json.RawMessage `json:"PriceList"`
	NextToken string            `json:"NextToken"`
}

type priceProduct struct {
	Product struct {
		Attributes struct {
			InstanceType string `json:"instanceType"`
		} `json:"attributes"`
	} `json:"product"`
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

// getOnDemandPrice fetches the hourly on-demand USD price for one instance
// type in the given region via the Pricing API.
func (p *pricingClient) getOnDemandPrice(ctx context.Context, region, instanceType string) (float64, error) {
	body, _ := json.Marshal(getProductsRequest{
		ServiceCode: "AmazonEC2",
		Filters: []priceTerm{
			{Type: "TERM_MATCH", Field: "instanceType", Value: instanceType},
			{Type: "TERM_MATCH", Field: "regionCode", Value: region},
			{Type: "TERM_MATCH", Field: "operatingSystem", Value: "Linux"},
			{Type: "TERM_MATCH", Field: "capacitystatus", Value: "Used"},
			{Type: "TERM_MATCH", Field: "tenancy", Value: "Shared"},
		},
		MaxResults: 1,
	})

	host := "api.pricing." + region + ".amazonaws.com"
	uri := "/"
	payloadHash := sha256hex(string(body))
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := amzDate[:8]
	headers := map[string]string{
		"content-type":         "application/x-amz-json-1.1",
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	auth := signV4("POST", host, uri, region, "pricing",
		p.cred.AccessKeyID, p.cred.SecretAccessKey, payloadHash, amzDate, dateStamp, headers)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", "https://"+host+uri, strings.NewReader(string(body)))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/x-amz-json-1.1")
		req.Header.Set("X-Amz-Target", "AWSPriceListService.GetProducts")
		req.Header.Set("X-Amz-Date", amzDate)
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
		req.Header.Set("Authorization", auth)

		resp, err := p.http.Do(req)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
			}
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return 0, err
		}
		if resp.StatusCode >= 300 {
			return 0, fmt.Errorf("aws pricing http %d: %s", resp.StatusCode, truncate(string(raw), 500))
		}
		var gpr getProductsResponse
		if err := json.Unmarshal(raw, &gpr); err != nil {
			return 0, fmt.Errorf("aws pricing decode: %w", err)
		}
		for _, pl := range gpr.PriceList {
			var prod priceProduct
			if err := json.Unmarshal(pl, &prod); err != nil {
				continue
			}
			if prod.Product.Attributes.InstanceType != instanceType {
				continue
			}
			for _, onDemand := range prod.Terms.OnDemand {
				for _, dim := range onDemand.PriceDimensions {
					if p, err := parsePrice(dim.PricePerUnit.USD); err == nil && p > 0 {
						return p, nil
					}
				}
			}
		}
		return 0, fmt.Errorf("aws pricing: no on-demand price for %s in %s", instanceType, region)
	}
	return 0, lastErr
}
