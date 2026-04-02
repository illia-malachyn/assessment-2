package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCustomer_SendsPOSTToEscapedPath(t *testing.T) {
	var gotMethod string
	var gotEscapedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewHTTPBillingClient(srv.URL+"/api", srv.Client())
	err := client.ValidateCustomer(context.Background(), "cust/alpha beta")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/validate/cust%2Falpha%20beta", gotEscapedPath)
}

func TestValidateCustomer_Non200ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewHTTPBillingClient(srv.URL, srv.Client())
	err := client.ValidateCustomer(context.Background(), "cust-1")

	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected status 400")
}

func TestProcessRefund_SendsJSONPayload(t *testing.T) {
	type refundRequest struct {
		SubscriptionID string `json:"subscription_id"`
		AmountCents    int64  `json:"amount_cents"`
	}

	var gotMethod string
	var gotPath string
	var gotContentType string
	var gotPayload refundRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotContentType = r.Header.Get("Content-Type")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotPayload))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewHTTPBillingClient(srv.URL+"/v1/", srv.Client())
	err := client.ProcessRefund(context.Background(), "sub-1", 2000)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v1/refund", gotPath)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "sub-1", gotPayload.SubscriptionID)
	assert.Equal(t, int64(2000), gotPayload.AmountCents)
}

func TestProcessRefund_Non200ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewHTTPBillingClient(srv.URL, srv.Client())
	err := client.ProcessRefund(context.Background(), "sub-1", 2000)

	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected status 500")
}
