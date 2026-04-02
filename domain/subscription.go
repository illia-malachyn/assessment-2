package domain

import (
	"regexp"
	"time"
)

// Status is the subscription lifecycle state.
type Status string

const (
	StatusActive    Status = "ACTIVE"
	StatusCancelled Status = "CANCELLED"
)

// safeID matches alphanumeric characters, hyphens, and underscores only.
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// billingPeriodDays is the fixed pro-rata refund window.
const billingPeriodDays int64 = 30

// Subscription is the domain aggregate.
type Subscription struct {
	id           string // TODO: we may use types instead of strings so we can validate the input for them more cleanly
	customerID   string
	planID       string
	priceInCents int64
	status       Status
	createdAt    time.Time
	cancelledAt  *time.Time
	events       []DomainEvent
}

// NewSubscription validates inputs and returns an always-valid aggregate.
// The now parameter must come from the injected clock — never call time.Now() here.
func NewSubscription(id, customerID, planID string, priceInCents int64, now time.Time) (*Subscription, error) {
	if id == "" || customerID == "" || planID == "" {
		return nil, ErrInvalidInput
	}

	if !safeID.MatchString(id) || !safeID.MatchString(customerID) || !safeID.MatchString(planID) {
		return nil, ErrInvalidInput
	}

	if priceInCents <= 0 {
		return nil, ErrInvalidPrice
	}

	s := &Subscription{
		id:           id,
		customerID:   customerID,
		planID:       planID,
		priceInCents: priceInCents,
		status:       StatusActive,
		createdAt:    now,
	}

	s.recordEvent(SubscriptionCreatedEvent{
		SubscriptionID: id,
		CustomerID:     customerID,
		PlanID:         planID,
		PriceInCents:   priceInCents,
		CreatedAt:      now,
	})
	return s, nil
}

// Cancel enforces the already-cancelled invariant, computes a pro-rata refund
// in int64 cents using integer arithmetic only, and records the domain event.
// The now parameter must come from the injected clock — never call time.Now() here.
func (s *Subscription) Cancel(now time.Time) error {
	if s.status == StatusCancelled {
		return ErrAlreadyCancelled
	}

	// Integer-only refund arithmetic.
	daysUsed := int64(now.Sub(s.createdAt).Hours()) / 24
	if daysUsed < 0 {
		daysUsed = 0
	}
	if daysUsed > billingPeriodDays {
		daysUsed = billingPeriodDays
	}
	daysRemaining := billingPeriodDays - daysUsed
	refundCents := s.priceInCents * daysRemaining / billingPeriodDays

	s.status = StatusCancelled
	t := now
	s.cancelledAt = &t

	s.recordEvent(SubscriptionCancelledEvent{
		SubscriptionID:    s.id,
		CustomerID:        s.customerID,
		RefundAmountCents: refundCents,
		CancelledAt:       now,
	})
	return nil
}

// FlushEvents drains and returns all accumulated domain events.
// After this call s.events is nil — a second call returns nil.
func (s *Subscription) FlushEvents() []DomainEvent {
	events := s.events
	s.events = nil
	return events
}

// recordEvent appends a domain event to the aggregate's internal slice.
func (s *Subscription) recordEvent(e DomainEvent) {
	s.events = append(s.events, e)
}

func (s *Subscription) ID() string {
	return s.id
}

func (s *Subscription) CustomerID() string {
	return s.customerID
}

func (s *Subscription) PlanID() string {
	return s.planID
}

func (s *Subscription) PriceInCents() int64 {
	return s.priceInCents
}

func (s *Subscription) Status() Status {
	return s.status
}

func (s *Subscription) CreatedAt() time.Time {
	return s.createdAt
}

func (s *Subscription) CancelledAt() *time.Time {
	return s.cancelledAt
}
