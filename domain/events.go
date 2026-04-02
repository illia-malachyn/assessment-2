package domain

import "time"

// DomainEvent is a sealed marker interface.
// The unexported method ensures only the domain package can satisfy it.
type DomainEvent interface {
	domainEvent()
}

// SubscriptionCreatedEvent is emitted by NewSubscription on successful creation.
type SubscriptionCreatedEvent struct {
	SubscriptionID string
	CustomerID     string
	PlanID         string
	PriceInCents   int64
	CreatedAt      time.Time
}

func (SubscriptionCreatedEvent) domainEvent() {}

// SubscriptionCancelledEvent is emitted by Cancel on successful cancellation.
type SubscriptionCancelledEvent struct {
	SubscriptionID    string
	CustomerID        string
	RefundAmountCents int64
	CancelledAt       time.Time
}

func (SubscriptionCancelledEvent) domainEvent() {}
