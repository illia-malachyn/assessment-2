package contracts

import (
	"context"

	"github.com/candidate/subscription-service/domain"
)

// Committer represents a pending repository mutation that has not yet been applied.
// Call Commit to apply the write.
type Committer interface {
	Commit(ctx context.Context) error
}

// Repository is the persistence port.
type Repository interface {
	// FindByID returns domain.ErrNotFound if the subscription does not exist.
	FindByID(ctx context.Context, id string) (*domain.Subscription, error)

	// Save returns a Committer. We never write directly inside Save.
	Save(ctx context.Context, sub *domain.Subscription) (Committer, error)
}
