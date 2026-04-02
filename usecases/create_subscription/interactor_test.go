package create_subscription_test

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
	create "github.com/candidate/subscription-service/usecases/create_subscription"
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

func TestCreateSubscription_Execute(t *testing.T) {
	fixedClock := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name           string
		input          create.CreateInput
		repo           *fakeRepo
		billing        *fakeBillingClient
		wantErr        bool
		wantErrIs      error
		wantIDNonEmpty bool
	}{
		{
			name: "happy path returns non-empty subscription ID",
			input: create.CreateInput{
				ID:           "sub-1",
				CustomerID:   "cust-1",
				PlanID:       "plan-1",
				PriceInCents: 3000,
			},
			repo:           &fakeRepo{},
			billing:        &fakeBillingClient{},
			wantErr:        false,
			wantIDNonEmpty: true,
		},
		{
			name: "ValidateCustomer error propagated",
			input: create.CreateInput{
				ID:           "sub-1",
				CustomerID:   "cust-bad",
				PlanID:       "plan-1",
				PriceInCents: 3000,
			},
			repo:    &fakeRepo{},
			billing: &fakeBillingClient{validateErr: errors.New("invalid customer")},
			wantErr: true,
		},
		{
			name: "domain invalid input propagated",
			input: create.CreateInput{
				ID:           "", // empty ID triggers domain.ErrInvalidInput
				CustomerID:   "cust-1",
				PlanID:       "plan-1",
				PriceInCents: 3000,
			},
			repo:      &fakeRepo{},
			billing:   &fakeBillingClient{},
			wantErr:   true,
			wantErrIs: domain.ErrInvalidInput,
		},
		{
			name: "repo Save error propagated",
			input: create.CreateInput{
				ID:           "sub-1",
				CustomerID:   "cust-1",
				PlanID:       "plan-1",
				PriceInCents: 3000,
			},
			repo:    &fakeRepo{saveErr: errors.New("db error")},
			billing: &fakeBillingClient{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			interactor := create.NewInteractor(tc.repo, tc.billing, fixedClock)
			id, err := interactor.Execute(context.Background(), tc.input)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrIs != nil {
					assert.ErrorIs(t, err, tc.wantErrIs)
				}

				assert.Empty(t, id)
				return
			}

			require.NoError(t, err)
			if tc.wantIDNonEmpty {
				assert.NotEmpty(t, id)
			}

			assert.True(t, tc.repo.saveCalled)
		})
	}
}
