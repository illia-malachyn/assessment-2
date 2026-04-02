package cancel_subscription

import (
	"context"

	"github.com/candidate/subscription-service/contracts"
	"github.com/candidate/subscription-service/domain"
)

// Interactor orchestrates the cancel subscription use case.
type Interactor struct {
	repo          contracts.Repository
	billingClient contracts.BillingClient
	clock         contracts.Clock
}

// NewInteractor constructs an Interactor with all required dependencies injected.
func NewInteractor(repo contracts.Repository, billing contracts.BillingClient, clock contracts.Clock) *Interactor {
	return &Interactor{repo: repo, billingClient: billing, clock: clock}
}

// Execute runs the cancel subscription flow in strict order:
//  1. Load the aggregate via FindByID (propagates ErrNotFound transparently)
//  2. Cancel the aggregate (propagates ErrAlreadyCancelled transparently)
//  3. Persist the cancelled aggregate via Save
//  4. Drain events after Save (side effects fire only after persistence)
//  5. Dispatch each event through handleEvent (refund errors propagated)
func (i *Interactor) Execute(ctx context.Context, subscriptionID string) error {
	sub, err := i.repo.FindByID(ctx, subscriptionID)
	if err != nil {
		return err
	}

	if err := sub.Cancel(i.clock.Now()); err != nil {
		return err
	}

	committer, err := i.repo.Save(ctx, sub)
	if err != nil {
		return err
	}

	if err := committer.Commit(ctx); err != nil {
		return err
	}

	// TODO: Move refund dispatch to an outbox/async processor or retry?
	events := sub.FlushEvents()
	for _, event := range events {
		if err := i.handleEvent(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

// handleEvent dispatches a domain event to the appropriate side effect handler.
// A cancellation event triggers a billing refund.
func (i *Interactor) handleEvent(ctx context.Context, event domain.DomainEvent) error {
	switch e := event.(type) {
	case domain.SubscriptionCancelledEvent:
		return i.billingClient.ProcessRefund(ctx, e.SubscriptionID, e.RefundAmountCents)
	}

	return nil
}
