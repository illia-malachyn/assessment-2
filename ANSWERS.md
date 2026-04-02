# Architecture Questions - Answers

---

## Q1: Where should the refund HTTP call happen?

**Answer: C - In the service layer after `committer.Commit()` succeeds**, implemented via event dispatch in the usecase.

### How our implementation does it

After `committer.Commit(ctx)` succeeds, the usecase calls `sub.FlushEvents()` to drain domain events, then dispatches each event. A `SubscriptionCancelledEvent` is routed to `billingClient.ProcessRefund(ctx, subscriptionID, refundAmountCents)` - a call through the `contracts.BillingClient` interface, implemented by the HTTP adapter in `adapters/billing_client.go`. The usecase never imports `net/http`.

### Trade-off analysis

**Option A - Inside the Cancel usecase directly**
Acceptable, and functionally what our implementation does. The key constraint: the call must go through `contracts.BillingClient`, not `*http.Client`. If you mean "call the interface method in the usecase body," this is correct. If you mean "call `net/http` directly in the usecase," this reintroduces the layer violation.

**Option B - Inside the domain `Cancel()` method**
Wrong. Domain aggregates must have zero external dependencies. Calling an HTTP API from `domain.Subscription.Cancel()` violates domain purity completely - the domain package would import `net/http`, making it impossible to test without a real HTTP server and impossible to reuse in any non-HTTP context.

**Option C - In the service layer after `committer.Commit()` succeeds**
Correct. The DB write commits first, ensuring the state change is durable before the side effect fires. If the refund call fails, the subscription is already marked cancelled in the database - see Q2 for how to handle that case. The billing call goes through the `BillingClient` port (interface), not a raw HTTP client.

**Option D - As a separate usecase triggered by `SubscriptionCancelledEvent`**
Valid for async/event-sourced systems but overkill for this bounded context. It introduces eventual consistency: the subscription is cancelled but the refund may not happen immediately, and the caller receives success before the refund is attempted. This assessment's requirements are fully met by the synchronous option C.

---

## Q2: If `Cancel()` works but the refund API is down

### What should happen to the subscription status?

**The subscription stays `CANCELLED`.** Cancellation and refund are orthogonal concerns. Rolling back the cancellation because billing is temporarily unavailable creates a worse outcome: the customer cancelled but the service continues charging them. The state change must not be reversed due to a refund system outage.

### Should we retry? When? How many times?

Yes. Retry with exponential backoff and jitter, up to a configurable limit (3 retries is a common default). The retry logic belongs in the adapter (`adapters/billing_client.go`) or in an outbox processor - not in the usecase or domain.

Retry timing example: attempt 1 immediately, attempt 2 after 1 second, attempt 3 after 4 seconds, abandon after attempt 3.

### What if the refund fails after 3 retries?

1. **Log with full context** - subscription ID, customer ID, refund amount in cents, timestamp, error details. Structured logging so the record is queryable.
2. **Persist in a dead-letter mechanism** - a `failed_refunds` database table or a dead-letter queue (e.g., SQS DLQ). This creates a durable record that can be replayed.
3. **Alert on-call** - the failure must surface to an operator. A metric increment on `refund.failed` with an alerting threshold on error rate is the standard pattern.
4. **Never silently drop.** The customer is owed money. Silent failure is a contractual and regulatory violation.

### Our implementation

`ProcessRefund` error is always returned: the usecase propagates it to the caller. The subscription stays cancelled. The caller (e.g., HTTP handler) returns HTTP 500 and logs the failure with the subscription ID. Operations staff then trigger a manual or automated retry via the dead-letter record.

---

## Q3: Why is `time.Since()` wrong and why is float math dangerous?

### Problem 1: `time.Since()` is non-injectable

`time.Since(sub.StartDate)` calls the real system clock at the moment of execution. The result changes with every call and cannot be controlled in tests. Two calls milliseconds apart return different values. Any test verifying the refund calculation must either sleep for a known duration (slow, fragile), tolerate ranges (imprecise), or manipulate the system clock (not possible in standard Go tests without external tooling).

The correct pattern is to pass `now time.Time` as a parameter sourced from an injected clock. The delta `now.Sub(sub.StartDate)` uses only the parameters - no system clock dependency.

### Problem 2: `float64` arithmetic is dangerous for money

`float64` uses IEEE 754 binary fractions. Most decimal fractions have no exact binary representation: in Go, `0.1 + 0.2` evaluates to `0.30000000000000004`. For a price of $29.99 (2999 cents):

```
// float64 result (wrong)
2999.0 * (30.0 - 5.0) / 30.0 = 2499.1666...   ← not a whole number of cents

// int64 result (correct)
2999 * 25 / 30 = 2499                           ← exact integer arithmetic
```

The float value `2499.1666...` cannot be expressed as a real currency amount, and no rounding is performed before it is sent to the billing API.

### Correct rewrite

```go
// In the domain aggregate - receives now from the injected clock.
const billingPeriodDays int64 = 30

func (s *Subscription) Cancel(now time.Time) error {
    if s.status == StatusCancelled {
        return ErrAlreadyCancelled
    }
    daysUsed := int64(now.Sub(s.createdAt).Hours()) / 24
    if daysUsed > billingPeriodDays {
        daysUsed = billingPeriodDays
    }
    daysRemaining := billingPeriodDays - daysUsed
    refundCents := s.priceInCents * daysRemaining / billingPeriodDays
    s.status = StatusCancelled
    s.events = append(s.events, SubscriptionCancelledEvent{
        SubscriptionID:   s.id,
        RefundAmountCents: refundCents,
    })
    return nil
}

// In the usecase - clock is injected via contracts.Clock interface.
func (i *Interactor) Execute(ctx context.Context, subscriptionID string) error {
    sub, committer, err := i.repo.FindByID(ctx, subscriptionID)
    if err != nil {
        return err
    }
    if err := sub.Cancel(i.clock.Now()); err != nil {
        return err
    }
    if err := committer.Commit(ctx); err != nil {
        return err
    }
    for _, event := range sub.FlushEvents() {
        if err := i.handleEvent(ctx, event); err != nil {
            return err
        }
    }
    return nil
}
```

The domain aggregate receives `now time.Time` from the usecase, which obtains it from `i.clock.Now()`. In tests, `clockwork.NewFakeClockAt(knownTime)` is injected to assert exact cent values without sleeping or tolerating ranges.

---

## Q4: Test design for `CancelSubscription`

### Four cases to cover

| Case | Repo state | Expected outcome |
|------|-----------|------------------|
| Success path | Active subscription, day 15 of 30 | No error, refund = `priceInCents * 15 / 30`, billing called |
| Already cancelled | Subscription already cancelled | `errors.Is(err, domain.ErrAlreadyCancelled)`, billing not called |
| Not found | No subscription with that ID | `errors.Is(err, domain.ErrNotFound)` |
| Refund API down | Active subscription, billing returns error | Error propagated, subscription still cancelled in repo |

### Test structure

```go
func TestCancelSubscription(t *testing.T) {
    // Pin time: subscription started day 1, cancel on day 16 -> 15 days used
    startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
    fixedNow  := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)
    // priceInCents=3000, daysUsed=15, refund = 3000*15/30 = 1500

    tests := []struct {
        name          string
        setupRepo     func() *fakeRepo
        setupBilling  func() *fakeBilling
        wantErr       error
        wantRefundCents int64
    }{
        {
            name: "success - refund calculated and dispatched",
            setupRepo: func() *fakeRepo {
                return newFakeRepoWithSubscription(startDate, 3000)
            },
            setupBilling:    func() *fakeBilling { return &fakeBilling{} },
            wantRefundCents: 1500,
        },
        {
            name: "already cancelled",
            setupRepo: func() *fakeRepo {
                return newFakeRepoWithCancelledSubscription(startDate, 3000)
            },
            setupBilling: func() *fakeBilling { return &fakeBilling{} },
            wantErr:      domain.ErrAlreadyCancelled,
        },
        {
            name:         "not found",
            setupRepo:    func() *fakeRepo { return newFakeRepoEmpty() },
            setupBilling: func() *fakeBilling { return &fakeBilling{} },
            wantErr:      domain.ErrNotFound,
        },
        {
            name: "refund API down - error propagates",
            setupRepo: func() *fakeRepo {
                return newFakeRepoWithSubscription(startDate, 3000)
            },
            setupBilling: func() *fakeBilling {
                return &fakeBilling{err: errors.New("billing unavailable")}
            },
            wantErr: errors.New("billing unavailable"),
        },
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            repo    := tc.setupRepo()
            billing := tc.setupBilling()
            clock   := clockwork.NewFakeClockAt(fixedNow)

            interactor := cancel_subscription.NewInteractor(repo, billing, clock)
            err := interactor.Execute(context.Background(), "sub-1")

            if tc.wantErr != nil {
                require.ErrorIs(t, err, tc.wantErr)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tc.wantRefundCents, billing.lastRefundCents)
            assert.True(t, billing.refundCalled)
        })
    }
}
```

### What to mock

- `contracts.Repository` - hand-rolled `fakeRepo` struct: holds a `*domain.Subscription` (or `nil` for not-found), returns it from `FindByID`, records Save calls
- `contracts.BillingClient` - hand-rolled `fakeBilling` struct: records `lastRefundCents` and `refundCalled`, returns configurable `err` from `ProcessRefund`
- `contracts.Clock` - `clockwork.NewFakeClockAt(fixedNow)` for deterministic time

### What to assert on each case

**Success path:**
- `require.NoError(t, err)`
- `assert.Equal(t, int64(1500), billing.lastRefundCents)` - exact cents, no float tolerance
- `assert.True(t, billing.refundCalled)`
- Optionally: `assert.Equal(t, domain.StatusCancelled, repo.savedSubscription.Status())`

**Already cancelled:**
- `require.ErrorIs(t, err, domain.ErrAlreadyCancelled)` - sentinel error, not string match
- `assert.False(t, billing.refundCalled)` - billing must not be called

**Not found:**
- `require.ErrorIs(t, err, domain.ErrNotFound)`

**Refund API down:**
- `require.Error(t, err)` - error propagated, not dropped
- The subscription is saved (repo.Commit was called) even though billing failed

---

## Q5: Business problems of ignoring billing API errors

The immediate crash - `defer resp.Body.Close()` panics on a nil `resp` - is only the surface symptom. The deeper business problems are:

### 1. Invalid customers can create subscriptions

If the billing API is temporarily unavailable and the error is discarded, `result.Valid` retains its zero value (`false`). The customer is incorrectly rejected. In a race condition where the struct is pre-populated, they might be incorrectly accepted. Either outcome is wrong. Silent failures make the system's behaviour under partial failure undefined rather than deterministic.

### 2. Fraud and compliance exposure

The billing system is the authoritative source for customer validity: payment method on file, fraud flags, KYC status, sanctions screening. Bypassing its response - even accidentally - means unverified, fraudulent, or sanctioned customers can create subscriptions. This is not a code quality issue; it is a regulatory and financial liability that can result in fines, chargebacks, and account suspension by the payment processor.

### 3. Cascading data integrity failures

A subscription created for an invalid customer will fail at billing time (charge attempt), generating a bad debt record. Downstream systems that consume the subscriptions table - MRR calculations, churn metrics, cohort analysis, dunning workflows - will be corrupted by subscriptions that should never have existed.

### 4. Silent operational failures are invisible to monitoring

A discarded error leaves no trace in logs, metrics, or alerting. The on-call team has no signal when the billing API degrades. Proper error propagation:

```go
if err != nil {
    return nil, fmt.Errorf("billing: ValidateCustomer: %w", err)
}
```

...allows the HTTP handler to return HTTP 503, the load balancer to log the upstream failure, and alerting rules to fire on error rate spikes. Discarding the error disables all of this observability.

### 5. Transient errors become permanent silent failures

A network timeout or DNS hiccup is transient - the correct action is to return an error and let the caller retry. With the error discarded, the retry never happens. The operation fails silently with the appearance of success (or a misleading "invalid customer" rejection), and the transient condition appears to have no effect at all.
