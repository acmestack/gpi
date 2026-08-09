package aliyun

import (
	"context"
	"sync"
)

// priceWorkers bounds concurrent price API calls per batch.
const priceWorkers = 8

// priceResp is the DescribePrice response shape.
type priceResp struct {
	PriceInfo struct {
		Price struct {
			OriginalPrice float64 `json:"OriginalPrice"`
			TradePrice    float64 `json:"TradePrice"`
		} `json:"Price"`
	} `json:"PriceInfo"`
}

// spotHistoryItem is one entry of DescribeSpotPriceHistory.
type spotHistoryItem struct {
	InstanceType string  `json:"InstanceType"`
	SpotPrice    float64 `json:"SpotPrice"`
	ZoneId       string  `json:"ZoneId"`
	Timestamp    string  `json:"Timestamp"`
}

type spotHistoryResp struct {
	SpotPrices struct {
		SpotPriceType []spotHistoryItem `json:"SpotPriceType"`
	} `json:"SpotPrices"`
}

// DescribeOnDemandPrice returns the hourly on-demand (PostPaid) price for the
// given instance types in region. Types without a price are omitted. A single
// type failing (e.g. not offered) does not abort the whole batch: the error is
// only returned when no type could be priced at all. Queries run concurrently.
func (c *Client) DescribeOnDemandPrice(ctx context.Context, region string, instanceTypes []string) (map[string]float64, error) {
	out := map[string]float64{}
	var (
		mu       sync.Mutex
		firstErr error
	)
	parallelPrice(ctx, instanceTypes, priceWorkers, func(ctx context.Context, it string) error {
		params := map[string]string{
			"RegionId":            region,
			"PriceUnit":           "Hour",
			"ResourceType":        "instance",
			"InstanceType":        it,
			"SystemDisk.Category": "cloud_essd",
		}
		var resp priceResp
		if err := c.call(ctx, "DescribePrice", params, &resp); err != nil {
			return err
		}
		price := resp.PriceInfo.Price.TradePrice
		if price <= 0 {
			price = resp.PriceInfo.Price.OriginalPrice
		}
		if price > 0 {
			mu.Lock()
			out[it] = price
			mu.Unlock()
		}
		return nil
	}, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	})
	if len(out) == 0 {
		return out, firstErr
	}
	return out, nil
}

// DescribeSpotPriceHistory returns the most recent spot price for each given
// instance type in region (latest history entry across all zones). Types with
// no spot history are omitted. A single type failing does not abort the batch.
func (c *Client) DescribeSpotPriceHistory(ctx context.Context, region string, instanceTypes []string) (map[string]float64, error) {
	out := map[string]float64{}
	var (
		mu       sync.Mutex
		firstErr error
	)
	parallelPrice(ctx, instanceTypes, priceWorkers, func(ctx context.Context, it string) error {
		params := map[string]string{
			"RegionId":     region,
			"NetworkType":  "vpc",
			"InstanceType": it,
			"Offset":       "1",
			"PageSize":     "20",
		}
		var resp spotHistoryResp
		if err := c.call(ctx, "DescribeSpotPriceHistory", params, &resp); err != nil {
			return err
		}
		// The API returns newest entries first; take the first for this type.
		for _, item := range resp.SpotPrices.SpotPriceType {
			if item.InstanceType != it {
				continue
			}
			if item.SpotPrice > 0 {
				mu.Lock()
				out[it] = item.SpotPrice
				mu.Unlock()
				break
			}
		}
		return nil
	}, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	})
	if len(out) == 0 {
		return out, firstErr
	}
	return out, nil
}

// parallelPrice runs fn over instanceTypes with up to workers concurrent
// goroutines. Each fn error is reported to onErr; errors do not stop the rest.
func parallelPrice(ctx context.Context, types []string, workers int, fn func(ctx context.Context, it string) error, onErr func(error)) {
	if len(types) == 0 {
		return
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, it := range types {
		it := it
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := fn(ctx, it); err != nil && onErr != nil {
				onErr(err)
			}
		}()
	}
	wg.Wait()
}
