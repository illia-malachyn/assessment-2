package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/candidate/subscription-service/domain"
)

func TestToRecord_MapsAggregateFields(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cancelledAt := createdAt.Add(24 * time.Hour)

	sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, createdAt)
	require.NoError(t, err)
	sub.FlushEvents()
	require.NoError(t, sub.Cancel(cancelledAt))

	rec := toRecord(sub)

	assert.Equal(t, "sub-1", rec.id)
	assert.Equal(t, "cust-1", rec.customerID)
	assert.Equal(t, "plan-1", rec.planID)
	assert.Equal(t, int64(3000), rec.priceInCents)
	assert.Equal(t, string(domain.StatusCancelled), rec.status)
	assert.Equal(t, createdAt, rec.createdAt)
	require.NotNil(t, rec.cancelledAt)
	assert.Equal(t, cancelledAt, *rec.cancelledAt)
}

func TestToDomain_ActiveRecord_ReconstructsActiveSubscription(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rec := subscriptionRecord{
		id:           "sub-1",
		customerID:   "cust-1",
		planID:       "plan-1",
		priceInCents: 3000,
		status:       string(domain.StatusActive),
		createdAt:    createdAt,
		cancelledAt:  nil,
	}

	sub, err := toDomain(rec)
	require.NoError(t, err)

	assert.Equal(t, "sub-1", sub.ID())
	assert.Equal(t, domain.StatusActive, sub.Status())
	assert.Nil(t, sub.CancelledAt())
	assert.Equal(t, createdAt, sub.CreatedAt())
}

func TestToDomain_CancelledRecord_ReconstructsCancelledAndDrainsEvents(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cancelledAt := createdAt.Add(10 * 24 * time.Hour)
	rec := subscriptionRecord{
		id:           "sub-1",
		customerID:   "cust-1",
		planID:       "plan-1",
		priceInCents: 3000,
		status:       string(domain.StatusCancelled),
		createdAt:    createdAt,
		cancelledAt:  &cancelledAt,
	}

	sub, err := toDomain(rec)
	require.NoError(t, err)

	assert.Equal(t, domain.StatusCancelled, sub.Status())
	require.NotNil(t, sub.CancelledAt())
	assert.Equal(t, cancelledAt, *sub.CancelledAt())
	assert.Nil(t, sub.FlushEvents())
}

func TestToDomain_InvalidRecord_ReturnsError(t *testing.T) {
	rec := subscriptionRecord{
		id:           "sub bad",
		customerID:   "cust-1",
		planID:       "plan-1",
		priceInCents: 3000,
		status:       string(domain.StatusActive),
		createdAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	sub, err := toDomain(rec)
	require.Error(t, err)
	assert.Nil(t, sub)
	assert.ErrorContains(t, err, "subscription_repo: toDomain")
	assert.ErrorIs(t, err, domain.ErrInvalidInput)
}
