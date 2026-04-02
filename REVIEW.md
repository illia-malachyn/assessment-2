# Code Review: Subscription Service

**Reviewer:** Senior Backend Engineer
**Subject:** `subscription` package — junior team submission
**Issues found:** 18
**Categories:** Layer Violations (5), Domain Purity (4), Error Handling (4), Money Handling (2), Testability (3)

---

## Issue 1: Infrastructure type `*sql.DB` on the service struct

- Category: Layer Violation
- Severity: CRITICAL
- Problem: `SubscriptionService` holds `*sql.DB` directly. No interface intermediates the persistence concern. Any layer that creates a `SubscriptionService` must supply a `*sql.DB` — it cannot substitute a fake repository, an in-memory store, or a different persistence backend without modifying the service itself.
- Why it matters: The service is permanently coupled to MySQL/SQLite driver semantics. Unit tests require a real database or a SQL mock library (`DATA-DOG/go-sqlmock`). Swapping to PostgreSQL or an in-memory store requires changing the service — a violation of the Open/Closed Principle.

## Issue 2: Infrastructure type `*http.Client` on the service struct

- Category: Layer Violation
- Severity: CRITICAL
- Problem: `SubscriptionService` holds `*http.Client`, a transport primitive. The billing system is coupled to HTTP as its protocol. There is no abstraction between the business logic and the transport layer.
- Why it matters: Introducing a `BillingClient` interface decouples the business logic from the transport entirely. Without it, testing any billing interaction requires an actual HTTP server or a custom `http.RoundTripper` mock — both are heavier than injecting a simple fake.

## Issue 3: Raw SQL inside business logic methods

- Category: Layer Violation
- Severity: CRITICAL
- Problem: `CreateSubscription` executes `INSERT INTO subscriptions VALUES (?, ?, ?, ?, ?, ?)` and `CancelSubscription` executes `UPDATE subscriptions SET status = 'CANCELLED' WHERE id = ?` inline. Persistence concerns — column mapping, query syntax, driver-specific placeholders — are embedded in the methods that enforce business rules.
- Why it matters: Mixing persistence and domain logic makes both harder to change independently. The SQL cannot be optimised, cached, or swapped without touching business rule code. A repository layer (Repository interface + concrete implementation) is the correct separation.

## Issue 4: Direct HTTP call in `CreateSubscription` (customer validation)

- Category: Layer Violation
- Severity: CRITICAL
- Problem: `s.client.Get("https://api.billing.com/validate/" + req.CustomerID)` is a hardcoded HTTP call with a hardcoded URL inside a business method. The URL, the protocol, and the response shape are all infrastructure details that leak into the domain service.
- Why it matters: The correct abstraction is `BillingClient.ValidateCustomer(ctx, customerID)` — a port defined as an interface, implemented by an HTTP adapter. The usecase never sees `net/http`.

## Issue 5: Direct HTTP call in `CancelSubscription` (refund)

- Category: Layer Violation
- Severity: CRITICAL
- Problem: `s.client.Post("https://api.billing.com/refund", ...)` is a raw HTTP call inside the cancellation method. The refund belongs in the billing port (`BillingClient.ProcessRefund`), invoked via the interface, not as a raw `http.Post` inside business logic.
- Why it matters: The refund HTTP call leaks transport details, response parsing, and error handling into the business layer. Any change to the billing API endpoint, authentication scheme, or retry policy requires modifying business-layer code.

## Issue 6: All `Subscription` fields exported

- Category: Domain Purity
- Severity: CRITICAL
- Problem: `Subscription.ID`, `.CustomerID`, `.PlanID`, `.Price`, `.Status`, and `.StartDate` are all exported. Any package can write `sub.Status = "ACTIVE"` directly, bypassing all business rules and invariants.
- Why it matters: A domain aggregate must own its state transitions. Exported fields allow callers to produce invalid aggregates (e.g., cancelled subscription with a non-zero price, or a future start date) without going through the validated constructors and methods. All fields must be unexported with public getters for read access only.

## Issue 7: `Status` is a raw `string`, not a typed constant

- Category: Domain Purity
- Severity: WARNING
- Problem: `Status string` accepts any value — `"active"`, `"Active"`, `"ACTIVE"`, `""`, `"banana"`. The type provides no safety. Magic string literals `"ACTIVE"` and `"CANCELLED"` appear in multiple places, making inconsistency inevitable.
- Why it matters: A `type Status string` with package-level constants (`StatusActive`, `StatusCancelled`) makes illegal states unrepresentable at the type level. Exhaustive `switch` checks become possible. Refactoring a status name becomes a single change, not a grep-and-replace.

## Issue 8: Business rules for cancellation live on the service, not the aggregate

- Category: Domain Purity
- Severity: CRITICAL
- Problem: The "already cancelled" check and the refund arithmetic are implemented in `SubscriptionService.CancelSubscription`. These are domain invariants — only the aggregate knows its own state and the rules governing state transitions.
- Why it matters: Domain logic on the service cannot be tested without wiring up infrastructure (database, HTTP). Two service implementations could diverge in their cancellation rules. The correct placement is `domain.Subscription.Cancel(now time.Time) error` — the aggregate enforces its own invariants.

## Issue 9: `time.Now()` called directly during aggregate creation

- Category: Domain Purity
- Severity: WARNING
- Problem: `StartDate: time.Now()` in `CreateSubscription` ties the created-at timestamp to the real system clock. The aggregate (or the method constructing it) cannot be tested with deterministic time.
- Why it matters: The correct pattern passes `now time.Time` from an injected `Clock` interface so tests can pin time to a known value and assert exact timestamps. `time.Now()` in construction code makes snapshot testing and time-based assertions impossible without system clock manipulation.

## Issue 10: HTTP transport error discarded with blank identifier

- Category: Error Handling
- Severity: CRITICAL
- Problem: `resp, _ := s.client.Get(...)` silently discards the transport error. If the network is down, DNS fails, or the server is unreachable, `resp` is `nil`. The next line `defer resp.Body.Close()` panics immediately with a nil pointer dereference.
- Why it matters: Beyond the crash, a discarded transport error means the billing system's unavailability is invisible to the caller. No HTTP 503 is returned, no alert fires, and the operation silently fails in an undefined state. Every network call must check its error return.

## Issue 11: JSON decode error discarded

- Category: Error Handling
- Severity: WARNING
- Problem: `json.NewDecoder(resp.Body).Decode(&result)` discards its error return. If the billing API returns a non-JSON body (HTML error page from a proxy, rate-limit response), `result.Valid` stays `false` and the customer is silently treated as invalid — with no indication of why.
- Why it matters: Silent decode failures are operationally invisible. The customer sees a generic "invalid customer" error with no diagnostic information. The on-call engineer sees no structured error in logs. The correct handling is `if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { return nil, fmt.Errorf("billing: decode response: %w", err) }`.

## Issue 12: `row.Scan` error discarded in `CancelSubscription`

- Category: Error Handling
- Severity: CRITICAL
- Problem: `row.Scan(&sub.ID, &sub.CustomerID, &sub.PlanID, &sub.Price, &sub.Status, &sub.StartDate)` discards its error. If the subscription ID does not exist, `sql.ErrNoRows` is swallowed. The struct remains zero-valued: `sub.Status == ""`, so the already-cancelled check is false. The code proceeds to calculate a refund on a zero-price zero-date subscription, then attempt to cancel a subscription that does not exist.
- Why it matters: This is a silent data-corruption path. A request for a non-existent subscription ID produces a spurious `UPDATE` that affects zero rows — with no error returned to the caller. `sql.ErrNoRows` must be mapped to a domain sentinel error (`domain.ErrNotFound`) and returned explicitly.

## Issue 13: Refund HTTP call result entirely discarded

- Category: Error Handling
- Severity: CRITICAL
- Problem: `s.client.Post("https://api.billing.com/refund", ...)` — both return values (response and error) are discarded. The subscription is marked CANCELLED even if the refund call failed. The customer loses their subscription but never receives a refund.
- Why it matters: This is the most consequential error handling failure in the file. A lost refund is a financial liability, a customer support escalation, and potentially a regulatory violation. The result of every payment API call must be checked and propagated. The cancellation state change must be coordinated with the refund outcome.

## Issue 14: `Price` stored as `float64`

- Category: Money Handling
- Severity: CRITICAL
- Problem: `Price float64` uses IEEE 754 double precision for a monetary value. Floating point cannot represent most decimal fractions exactly: `0.1 + 0.2 = 0.30000000000000004` in Go. For a subscription priced at $29.99, the float64 representation is not exactly 29.99.
- Why it matters: The standard for monetary values in any financial system is integer cents (`int64`). A price of $29.99 is stored as `2999`. All arithmetic is exact integer arithmetic. There is no representation error, no rounding accumulation, and no ambiguity about what value will be serialised to JSON.

## Issue 15: Refund arithmetic uses `float64` division

- Category: Money Handling
- Severity: CRITICAL
- Problem: `refundAmount := sub.Price * (30 - daysUsed) / 30` mixes `float64` price with `float64` days. The result is a float like `9.9966666...` — not a whole number of cents. No rounding is performed before the value is serialised into the JSON payload sent to the billing API.
- Why it matters: Many payment APIs reject non-integer cent amounts. Those that accept floats round inconsistently (banker's rounding vs. floor vs. ceil). The correct implementation uses integer arithmetic throughout: `refundCents := priceInCents * daysRemaining / billingPeriodDays` — truncating integer division, exact for all inputs.

## Issue 16: `time.Since()` and `time.Now()` make time-dependent logic non-injectable

- Category: Testability
- Severity: WARNING
- Problem: `daysUsed := time.Since(sub.StartDate).Hours() / 24` calls the real system clock at execution time. The result changes with every call and is impossible to control in tests.
- Why it matters: Any test verifying refund calculation must either sleep for a known duration (slow, fragile), tolerate floating-point ranges (imprecise), or inject a fake time. With direct `time.Since`, none of these are clean. A `Clock` interface with `Now() time.Time` injected into the service resolves this entirely — tests pin the clock to an exact value and assert exact results without sleeping.

## Issue 17: `*sql.DB` concrete dependency makes unit tests require a real database

- Category: Testability
- Severity: CRITICAL
- Problem: Because `SubscriptionService` holds `*sql.DB` directly, any unit test for `CreateSubscription` or `CancelSubscription` must either start a real database or use a SQL driver mock library (such as `DATA-DOG/go-sqlmock`). Neither is clean. The service cannot be tested in isolation from its persistence infrastructure.
- Why it matters: A `Repository` interface injected into the service would allow tests to supply an in-memory fake implementing two methods. Tests run in milliseconds with no external process, no network, and no schema migration. Concrete infrastructure dependencies in the service layer make fast, isolated unit tests structurally impossible.

## Issue 18: `*http.Client` concrete dependency makes unit tests require a live HTTP server

- Category: Testability
- Severity: CRITICAL
- Problem: `SubscriptionService` holds `*http.Client` directly, so any test that exercises the billing validation or refund paths must either stand up a real HTTP server or implement a custom `http.RoundTripper`. Both approaches require significantly more test infrastructure than injecting a simple fake.
- Why it matters: A `BillingClient` interface with `ValidateCustomer` and `ProcessRefund` methods makes injection trivial — a two-field fake struct satisfies the interface and can be configured per test case. Without the interface, testing billing error paths (network down, invalid customer, refund failure) requires network-level mocking rather than simple struct initialization.

---

*This review covers the full `subscription` package as submitted. All 18 issues above have been corrected in the reference implementation: `domain/subscription.go`, `usecases/cancel_subscription/interactor.go`, `usecases/create_subscription/interactor.go`, `repo/subscription_repo.go`, and `adapters/billing_client.go`.*
