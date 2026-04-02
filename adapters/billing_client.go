package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/candidate/subscription-service/contracts"
)

// HTTPBillingClient implements contracts.BillingClient using net/http.
// It wraps an injected *http.Client so callers can configure timeouts and transport.
type HTTPBillingClient struct {
	baseURL    string
	httpClient *http.Client
}

var _ contracts.BillingClient = (*HTTPBillingClient)(nil)

// NewHTTPBillingClient constructs an HTTPBillingClient.
// If httpClient is nil a default client with a 10-second timeout is used.
func NewHTTPBillingClient(baseURL string, httpClient *http.Client) *HTTPBillingClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &HTTPBillingClient{baseURL: baseURL, httpClient: httpClient}
}

// ValidateCustomer sends a POST to {baseURL}/validate/{customerID}.
// Returns nil on HTTP 200, non-nil error on any other status code or transport failure.
func (c *HTTPBillingClient) ValidateCustomer(ctx context.Context, customerID string) error {
	endpoint := strings.TrimRight(c.baseURL, "/") + "/validate/" + url.PathEscape(customerID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("billing_client: ValidateCustomer: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("billing_client: ValidateCustomer: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("billing_client: ValidateCustomer: unexpected status %d", resp.StatusCode)
	}

	return nil
}

// ProcessRefund sends a POST to {baseURL}/refund with a JSON body.
// The body contains "subscription_id" (string) and "amount_cents" (int64).
// Returns nil on HTTP 200, non-nil error on any other status code or transport failure.
func (c *HTTPBillingClient) ProcessRefund(ctx context.Context, subscriptionID string, amountCents int64) error {
	body, err := json.Marshal(map[string]interface{}{
		"subscription_id": subscriptionID,
		"amount_cents":    amountCents, // int64 cents — never cast to float64
	})
	if err != nil {
		return fmt.Errorf("billing_client: ProcessRefund: marshal body: %w", err)
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/refund"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("billing_client: ProcessRefund: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("billing_client: ProcessRefund: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("billing_client: ProcessRefund: unexpected status %d", resp.StatusCode)
	}

	return nil
}
