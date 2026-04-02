package contracts

import "time"

// Clock abstracts wall-clock time. Inject into any usecase or service
// that needs the current time.
type Clock interface {
	Now() time.Time
}
