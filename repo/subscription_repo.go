package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/candidate/subscription-service/contracts"
	"github.com/candidate/subscription-service/domain"
)

// subscriptionRecord is the internal persistence model.
// It maps one-to-one to the subscriptions table columns.
type subscriptionRecord struct {
	id           string
	customerID   string
	planID       string
	priceInCents int64
	status       string
	createdAt    time.Time
	cancelledAt  *time.Time
}

// pendingUpsert is the contracts.Committer returned by Save.
// It holds a pre-mapped record and writes to the DB only when Commit is called.
type pendingUpsert struct {
	db     *sql.DB
	record subscriptionRecord
}

// Commit applies the pending upsert to the database.
func (p *pendingUpsert) Commit(ctx context.Context) error {
	_, err := p.db.ExecContext(
		ctx,
		`INSERT INTO subscriptions (id, customer_id, plan_id, price_in_cents, status, created_at, cancelled_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
		     status       = excluded.status,
		     cancelled_at = excluded.cancelled_at`,
		p.record.id,
		p.record.customerID,
		p.record.planID,
		p.record.priceInCents,
		p.record.status,
		p.record.createdAt,
		p.record.cancelledAt,
	)
	if err != nil {
		return fmt.Errorf("subscription_repo: Commit: %w", err)
	}

	return nil
}

// SubscriptionRepository implements contracts.Repository using database/sql.
type SubscriptionRepository struct {
	db *sql.DB
}

var _ contracts.Repository = (*SubscriptionRepository)(nil)

// NewSubscriptionRepository constructs a SubscriptionRepository with an injected *sql.DB.
func NewSubscriptionRepository(db *sql.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

// Save maps the aggregate to a persistence record and returns a Committer.
// It does NOT write to the database. Call Commit on the returned value to apply the write.
// The caller controls when the mutation hits the DB.
func (r *SubscriptionRepository) Save(_ context.Context, sub *domain.Subscription) (contracts.Committer, error) {
	record := toRecord(sub)
	return &pendingUpsert{db: r.db, record: record}, nil
}

// FindByID loads a subscription by its ID.
// Returns domain.ErrNotFound if no row exists for the given ID.
func (r *SubscriptionRepository) FindByID(ctx context.Context, id string) (*domain.Subscription, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, customer_id, plan_id, price_in_cents, status, created_at, cancelled_at
		 FROM subscriptions
		 WHERE id = ?`,
		id,
	)

	var rec subscriptionRecord
	err := row.Scan(
		&rec.id,
		&rec.customerID,
		&rec.planID,
		&rec.priceInCents,
		&rec.status,
		&rec.createdAt,
		&rec.cancelledAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("subscription_repo: FindByID: %w", err)
	}
	return toDomain(rec)
}

// toRecord maps a domain.Subscription to a subscriptionRecord using only public getters.
// No reflection or unsafe field access, the domain enforces its own invariants.
func toRecord(sub *domain.Subscription) subscriptionRecord {
	return subscriptionRecord{
		id:           sub.ID(),
		customerID:   sub.CustomerID(),
		planID:       sub.PlanID(),
		priceInCents: sub.PriceInCents(),
		status:       string(sub.Status()),
		createdAt:    sub.CreatedAt(),
		cancelledAt:  sub.CancelledAt(),
	}
}

// toDomain reconstructs a domain.Subscription from a persisted record.
// For cancelled subscriptions it calls Cancel to restore state, then immediately flushes
// the resulting event. That event is an artefact of reconstruction, not a new cancellation.
// This prevents the usecase from seeing a spurious SubscriptionCancelledEvent and dispatching
// a duplicate refund.
func toDomain(rec subscriptionRecord) (*domain.Subscription, error) {
	sub, err := domain.NewSubscription(
		rec.id,
		rec.customerID,
		rec.planID,
		rec.priceInCents,
		rec.createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("subscription_repo: toDomain: %w", err)
	}

	if rec.status == string(domain.StatusCancelled) && rec.cancelledAt != nil {
		// Re-apply Cancel to put the aggregate back in the cancelled state.
		// Cancel emits a SubscriptionCancelledEvent as a side effect — flush it immediately
		// so the usecase does not dispatch a duplicate refund.
		if cancelErr := sub.Cancel(*rec.cancelledAt); cancelErr != nil {
			return nil, fmt.Errorf("subscription_repo: toDomain: restore cancel: %w", cancelErr)
		}
		sub.FlushEvents() // drain the reconstruction-triggered event — not a real new cancellation
	}

	return sub, nil
}
