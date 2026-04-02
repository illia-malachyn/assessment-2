package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/candidate/subscription-service/domain"
)

func TestNewSubscription_ValidAggregateAndCreatedEvent(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, now)
	require.NoError(t, err)

	assert.Equal(t, "sub-1", sub.ID())
	assert.Equal(t, "cust-1", sub.CustomerID())
	assert.Equal(t, "plan-1", sub.PlanID())
	assert.Equal(t, int64(3000), sub.PriceInCents())
	assert.Equal(t, domain.StatusActive, sub.Status())
	assert.Equal(t, now, sub.CreatedAt())
	assert.Nil(t, sub.CancelledAt())

	events := sub.FlushEvents()
	require.Len(t, events, 1)

	created, ok := events[0].(domain.SubscriptionCreatedEvent)
	require.True(t, ok)
	assert.Equal(t, "sub-1", created.SubscriptionID)
	assert.Equal(t, "cust-1", created.CustomerID)
	assert.Equal(t, "plan-1", created.PlanID)
	assert.Equal(t, int64(3000), created.PriceInCents)
	assert.Equal(t, now, created.CreatedAt)
}

func TestNewSubscription_InvalidInputAndPrice(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	_, err := domain.NewSubscription("", "cust-1", "plan-1", 3000, now)
	assert.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = domain.NewSubscription("sub-1", "cust bad", "plan-1", 3000, now)
	assert.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = domain.NewSubscription("sub-1", "cust-1", "plan-1", 0, now)
	assert.ErrorIs(t, err, domain.ErrInvalidPrice)
}

func TestSubscription_Cancel_RefundAndInvariants(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		cancelAt        time.Time
		wantRefundCents int64
	}{
		{
			name:            "negative duration clamps to zero days used",
			cancelAt:        createdAt.Add(-24 * time.Hour),
			wantRefundCents: 3000,
		},
		{
			name:            "10 days used gets 20/30 refund",
			cancelAt:        createdAt.Add(10 * 24 * time.Hour),
			wantRefundCents: 2000,
		},
		{
			name:            "beyond billing period refunds zero",
			cancelAt:        createdAt.Add(45 * 24 * time.Hour),
			wantRefundCents: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, createdAt)
			require.NoError(t, err)
			sub.FlushEvents() // creation event is not part of cancellation assertions

			err = sub.Cancel(tc.cancelAt)
			require.NoError(t, err)

			assert.Equal(t, domain.StatusCancelled, sub.Status())
			require.NotNil(t, sub.CancelledAt())
			assert.Equal(t, tc.cancelAt, *sub.CancelledAt())

			events := sub.FlushEvents()
			require.Len(t, events, 1)

			cancelled, ok := events[0].(domain.SubscriptionCancelledEvent)
			require.True(t, ok)
			assert.Equal(t, "sub-1", cancelled.SubscriptionID)
			assert.Equal(t, "cust-1", cancelled.CustomerID)
			assert.Equal(t, tc.wantRefundCents, cancelled.RefundAmountCents)
			assert.Equal(t, tc.cancelAt, cancelled.CancelledAt)
		})
	}
}

func TestSubscription_Cancel_AlreadyCancelled(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, createdAt)
	require.NoError(t, err)
	sub.FlushEvents()

	require.NoError(t, sub.Cancel(createdAt.Add(24*time.Hour)))
	err = sub.Cancel(createdAt.Add(48 * time.Hour))
	assert.ErrorIs(t, err, domain.ErrAlreadyCancelled)
}

func TestSubscription_FlushEvents_DrainsBuffer(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, createdAt)
	require.NoError(t, err)

	first := sub.FlushEvents()
	require.Len(t, first, 1)

	second := sub.FlushEvents()
	assert.Nil(t, second)
}
