package create_subscription

import (
	"context"

	"github.com/candidate/subscription-service/contracts"
	"github.com/candidate/subscription-service/domain"
)

// CreateInput carries the parameters required to create a new subscription.
type CreateInput struct {
	ID           string
	CustomerID   string
	PlanID       string
	PriceInCents int64
}

// Interactor orchestrates the CreateSubscription use case.
// All dependencies are held as interface values for dependency inversion.
type Interactor struct {
	repo          contracts.Repository
	billingClient contracts.BillingClient
	clock         contracts.Clock
}

// NewInteractor constructs an Interactor with its required dependencies.
func NewInteractor(repo contracts.Repository, billing contracts.BillingClient, clock contracts.Clock) *Interactor {
	return &Interactor{repo: repo, billingClient: billing, clock: clock}
}

// Execute runs the CreateSubscription flow in strict order:
//  1. domain.NewSubscription - validates input and creates the aggregate
//  2. ValidateCustomer - verifies the customer exists in the billing system
//  3. repo.Save - persists the new aggregate
//  4. FlushEvents - drains domain events (no dispatcher in this usecase)
//  5. Return the subscription ID on success
func (i *Interactor) Execute(ctx context.Context, input CreateInput) (string, error) {
	sub, err := domain.NewSubscription(input.ID, input.CustomerID, input.PlanID, input.PriceInCents, i.clock.Now())
	if err != nil {
		return "", err
	}

	if err := i.billingClient.ValidateCustomer(ctx, input.CustomerID); err != nil {
		return "", err
	}

	committer, err := i.repo.Save(ctx, sub)
	if err != nil {
		return "", err
	}
	if err := committer.Commit(ctx); err != nil {
		return "", err
	}

	sub.FlushEvents()

	return sub.ID(), nil
}
