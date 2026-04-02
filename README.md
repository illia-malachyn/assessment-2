## Task: Architecture Design - Subscription Service

You're the tech lead. Design a complete Subscription Management bounded context.

### Given: Broken Implementation

The junior team submitted this. It has 12+ issues. Find them all.

```go
package subscription

import (
    "context"
    "database/sql"
    "encoding/json"
    "net/http"
    "time"
)

type SubscriptionService struct {
    db     *sql.DB
    client *http.Client
}

type Subscription struct {
    ID         string
    CustomerID string
    PlanID     string
    Price      float64
    Status     string
    StartDate  time.Time
}

func (s *SubscriptionService) CreateSubscription(ctx context.Context, req CreateRequest) (*Subscription, error) {
    // Validate customer with external API
    resp, _ := s.client.Get("https://api.billing.com/validate/" + req.CustomerID)
    defer resp.Body.Close()

    var result struct{ Valid bool }
    json.NewDecoder(resp.Body).Decode(&result)

    if !result.Valid {
        return nil, errors.New("invalid customer")
    }

    sub := &Subscription{
        ID:         uuid.New().String(),
        CustomerID: req.CustomerID,
        PlanID:     req.PlanID,
        Price:      req.Price,
        Status:     "ACTIVE",
        StartDate:  time.Now(),
    }

    _, err := s.db.ExecContext(ctx,
        "INSERT INTO subscriptions VALUES (?, ?, ?, ?, ?, ?)",
        sub.ID, sub.CustomerID, sub.PlanID, sub.Price, sub.Status, sub.StartDate)

    return sub, err
}

func (s *SubscriptionService) CancelSubscription(ctx context.Context, subID string) error {
    row := s.db.QueryRowContext(ctx, "SELECT * FROM subscriptions WHERE id = ?", subID)

    var sub Subscription
    row.Scan(&sub.ID, &sub.CustomerID, &sub.PlanID, &sub.Price, &sub.Status, &sub.StartDate)

    if sub.Status == "CANCELLED" {
        return errors.New("already cancelled")
    }

    // Calculate refund
    daysUsed := time.Since(sub.StartDate).Hours() / 24
    refundAmount := sub.Price * (30 - daysUsed) / 30

    // Call refund API
    payload, _ := json.Marshal(map[string]interface{}{"amount": refundAmount})
    s.client.Post("https://api.billing.com/refund", "application/json", bytes.NewReader(payload))

    _, err := s.db.ExecContext(ctx, "UPDATE subscriptions SET status = 'CANCELLED' WHERE id = ?", subID)
    return err
}
```

---

## Your Task

### Part 1: REVIEW.md

Find ALL issues. Categories:

- **Layer Violations** (at least 4)
- **Domain Purity** (at least 3)
- **Error Handling** (at least 2)
- **Money Handling** (at least 1)
- **Testability** (at least 2)

Format each issue:
```
## Issue N: [Title]
- Category: Layer Violation / Domain Purity / etc.
- Severity: CRITICAL / WARNING
- Problem: ...
- Why it matters: ...
```

### Part 2: Design the Correct Architecture

Create complete directory structure and implement:

```
internal/app/subscription/
├── domain/
│   ├── subscription.go    # Aggregate with private fields
│   ├── events.go          # SubscriptionCreatedEvent, CancelledEvent
│   └── errors.go
├── contracts/
│   ├── repository.go      # Interface
│   └── billing_client.go  # Interface for external API
├── usecases/
│   ├── create_subscription/
│   │   └── interactor.go
│   └── cancel_subscription/
│       └── interactor.go
├── repo/
│   └── subscription_repo.go
└── adapters/
    └── billing_client.go  # HTTP implementation
```

### Part 3: Implementation

Implement:
1. Domain aggregate with events
2. Repository that returns mutations
3. Both usecases following proper flow
4. Compile-time interface checks

---

## Questions - Answer in ANSWERS.md

**Q1:** The Cancel usecase needs to call an external refund API. Where should this HTTP call happen? Choose one and explain trade-offs:

- A) Inside the Cancel usecase
- B) Inside domain `Cancel()` method
- C) In service layer after `committer.Apply()` succeeds
- D) As separate usecase triggered by `SubscriptionCancelledEvent`

**Q2:** If `Cancel()` works but the refund API is down:
- What should happen to the subscription status?
- Should we retry? When? How many times?
- What if refund fails after 3 retries?

**Q3:** The buggy code does this:
```go
daysUsed := time.Since(sub.StartDate).Hours() / 24
refundAmount := sub.Price * (30 - daysUsed) / 30
```

Two problems:
1. Why is `time.Since()` wrong here?
2. Why is the math with floats dangerous?

Rewrite using int64 cents and injected clock.

**Q4:** Design a test for `CancelSubscription` that covers:
- Success path
- Already cancelled error
- Refund calculation correctness
- Outbox event created

Show test structure with what to mock and what to assert.

**Q5:** The code ignores errors:
```go
resp, _ := s.client.Get("https://api.billing.com/validate/" + req.CustomerID)
```

Besides nil pointer crash, what BUSINESS problem does this cause?

---

## Repository Structure

```
your-repo/
├── domain/
│   ├── subscription.go
│   ├── events.go
│   └── errors.go
├── contracts/
│   ├── repository.go
│   └── billing_client.go
├── usecases/
│   ├── create_subscription/
│   │   ├── interactor.go
│   │   └── interactor_test.go
│   └── cancel_subscription/
│       ├── interactor.go
│       └── interactor_test.go
├── repo/
│   └── subscription_repo.go
├── adapters/
│   └── billing_client.go
├── REVIEW.md
└── ANSWERS.md
```

---

## Evaluation

Your submission will be evaluated against our engineering standards document. Key areas:
- All 12+ issues identified and categorized
- Correct architecture with proper layer separation
- Domain aggregate with private fields and change tracking
- Repository returns mutations, never applies
- Usecase depends on interfaces, not concrete types
- Compile-time interface checks
- int64 cents for money, never float64
- External calls in service layer or event-driven
- Clock abstraction for time-dependent logic
