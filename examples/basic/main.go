package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ChiragRayani/resilix"
)

// Product is the domain type returned by the upstream API.
type Product struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Price int    `json:"price"`
}

// ProductClient wraps an HTTP client with a resilience pipeline.
type ProductClient struct {
	baseURL  string
	http     *http.Client
	pipeline *resilix.Pipeline
}

// NewProductClient wires up the resilience pipeline around a plain HTTP client.
func NewProductClient(baseURL string) *ProductClient {
	// Custom pipeline: 3s timeout, 2 retries only on 5xx/network errors,
	// circuit breaker tripping at 40% failure rate.
	pipeline := resilix.NewPipeline("product-api",
		resilix.NewTimeout(resilix.TimeoutConfig{
			Name:     "product-api.timeout",
			Duration: 5 * time.Second,
		}),
		resilix.NewRetry(resilix.RetryConfig{
			Name:        "product-api.retry",
			MaxAttempts: 3,
			Backoff:     resilix.ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 5*time.Second, 0.25),
			RetryIf:     isRetryableHTTPError,
		}),
		resilix.NewCircuitBreaker(resilix.CBConfig{
			Name:             "product-api.cb",
			FailureThreshold: 0.10,
			MinRequests:      9,
			OpenTimeout:      20 * time.Second,
			HalfOpenMax:      2,
			Window:           resilix.NewSlidingWindow(10*time.Second, 6),
		}),
		resilix.NewBulkhead(resilix.BulkheadConfig{
			Name:          "product-api.bulkhead",
			MaxConcurrent: 20,
			WaitTimeout:   500 * time.Millisecond,
		}),
	)

	return &ProductClient{
		baseURL:  baseURL,
		http:     &http.Client{Timeout: 10 * time.Second},
		pipeline: pipeline,
	}
}

// GetProduct fetches a product by ID with full resilience applied.
func (c *ProductClient) GetProduct(ctx context.Context, id int) (*Product, error) {
	product, err := resilix.Execute(ctx, c.pipeline,
		func(ctx context.Context) (*Product, error) {
			return c.doGetProduct(ctx, id)
		},
	)
	if err != nil {
		// Provide a cached/default value when the circuit is open.
		if errors.Is(err, resilix.ErrCircuitOpen) {
			log.Printf("circuit open for product %d, returning placeholder", id)
			return &Product{ID: id, Name: "unavailable", Price: 0}, nil
		}
		return nil, fmt.Errorf("get product %d: %w", id, err)
	}
	return product, nil
}

func (c *ProductClient) doGetProduct(ctx context.Context, id int) (*Product, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/products/%d", c.baseURL, id), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, &httpError{code: resp.StatusCode}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("product %d not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var p Product
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// httpError is a typed error so the retry predicate can inspect the status code.
type httpError struct{ code int }

func (e *httpError) Error() string { return fmt.Sprintf("http %d", e.code) }

// isRetryableHTTPError retries on 5xx and network errors, but not on 4xx.
func isRetryableHTTPError(err error, _ int) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.code >= 500
	}
	// Don't retry timeouts or cancellations
	if errors.Is(err, resilix.ErrTimeout) || errors.Is(err, context.Canceled) {
		return false
	}
	return err != nil
}

func main() {
	client := NewProductClient("https://api.example.com")
	ctx := context.Background()

	for _, id := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		p, err := client.GetProduct(ctx, id)
		if err != nil {
			log.Printf("error fetching product %d: %v", id, err)
			continue
		}
		log.Printf("product %d: %s ($%d)", p.ID, p.Name, p.Price)
	}
}
