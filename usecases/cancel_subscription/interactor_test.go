package cancel_subscription_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/candidate/subscription-service/contracts"
	"github.com/candidate/subscription-service/domain"
	cancel "github.com/candidate/subscription-service/usecases/cancel_subscription"
)

// fakeRepo is a hand-rolled test double satisfying contracts.Repository.
type fakeRepo struct {
	saveCalled bool
	saveErr    error
	findResult *domain.Subscription
	findErr    error
}

var _ contracts.Repository = (*fakeRepo)(nil)

type fakeCommitter struct{ err error }

func (f *fakeCommitter) Commit(_ context.Context) error { return f.err }

func (r *fakeRepo) Save(_ context.Context, _ *domain.Subscription) (contracts.Committer, error) {
	r.saveCalled = true
	return &fakeCommitter{err: r.saveErr}, nil
}

func (r *fakeRepo) FindByID(_ context.Context, _ string) (*domain.Subscription, error) {
	return r.findResult, r.findErr
}

// fakeBillingClient is a hand-rolled test double satisfying contracts.BillingClient.
type fakeBillingClient struct {
	validateErr      error
	processRefundErr error
	refundCalled     bool
	lastRefundAmount int64
}

var _ contracts.BillingClient = (*fakeBillingClient)(nil)

func (b *fakeBillingClient) ValidateCustomer(_ context.Context, _ string) error {
	return b.validateErr
}

func (b *fakeBillingClient) ProcessRefund(_ context.Context, _ string, amountCents int64) error {
	b.refundCalled = true
	b.lastRefundAmount = amountCents
	return b.processRefundErr
}

func TestCancelSubscription_Execute(t *testing.T) {
	fixedCreatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC) // exactly 10 days later

	fakeClock := clockwork.NewFakeClockAt(fixedNow)
	refundErr := errors.New("refund failed")

	newActiveSub := func(t *testing.T) *domain.Subscription {
		t.Helper()
		sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, fixedCreatedAt)
		require.NoError(t, err)
		sub.FlushEvents() // drain creation event so it does not interfere
		return sub
	}

	newCancelledSub := func(t *testing.T) *domain.Subscription {
		t.Helper()
		sub, err := domain.NewSubscription("sub-1", "cust-1", "plan-1", 3000, fixedCreatedAt)
		require.NoError(t, err)
		require.NoError(t, sub.Cancel(fixedCreatedAt))
		sub.FlushEvents()
		return sub
	}

	tests := []struct {
		name      string
		repo      func(t *testing.T) *fakeRepo
		billing   func() *fakeBillingClient
		wantErr   bool
		wantErrIs error
	}{
		{
			name:    "happy path returns nil and routes refund",
			repo: func(t *testing.T) *fakeRepo {
				return &fakeRepo{findResult: newActiveSub(t)}
			},
			billing: func() *fakeBillingClient { return &fakeBillingClient{} },
			wantErr: false,
		},
		{
			name:      "FindByID returns ErrNotFound",
			repo:      func(_ *testing.T) *fakeRepo { return &fakeRepo{findErr: domain.ErrNotFound} },
			billing:   func() *fakeBillingClient { return &fakeBillingClient{} },
			wantErr:   true,
			wantErrIs: domain.ErrNotFound,
		},
		{
			name:      "Cancel returns ErrAlreadyCancelled",
			repo:      func(t *testing.T) *fakeRepo { return &fakeRepo{findResult: newCancelledSub(t)} },
			billing:   func() *fakeBillingClient { return &fakeBillingClient{} },
			wantErr:   true,
			wantErrIs: domain.ErrAlreadyCancelled,
		},
		{
			name: "ProcessRefund error propagated",
			repo: func(t *testing.T) *fakeRepo {
				return &fakeRepo{findResult: newActiveSub(t)}
			},
			billing:   func() *fakeBillingClient { return &fakeBillingClient{processRefundErr: refundErr} },
			wantErr:   true,
			wantErrIs: refundErr,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := tc.repo(t)
			billing := tc.billing()
			interactor := cancel.NewInteractor(repo, billing, fakeClock)
			err := interactor.Execute(context.Background(), "sub-1")

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrIs != nil {
					assert.ErrorIs(t, err, tc.wantErrIs)
				}

				return
			}

			require.NoError(t, err)
			assert.True(t, billing.refundCalled)
			assert.Equal(t, int64(2000), billing.lastRefundAmount)
		})
	}
}
