package contracts

import "context"

// BillingClient is the billing system port.
type BillingClient interface {
	// ValidateCustomer returns nil if the customer is valid, non-nil error otherwise.
	ValidateCustomer(ctx context.Context, customerID string) error

	// ProcessRefund initiates a refund of amountCents.
	ProcessRefund(ctx context.Context, subscriptionID string, amountCents int64) error
}
