// Package budgetengine is a standalone, host-agnostic budget engine: it decides how much of a
// time-boxed, possibly multi-source budget has been spent, how fast it may be spent, and whether a
// proposed spend is allowed — without knowing anything about the host that embeds it.
//
// It has ZERO dependencies outside the Go standard library. That is the enforceable proof of its
// host-agnostic boundary: if go.mod ever grows a require, the boundary broke. Two very different
// hosts adopt it unchanged — Prism (a long-lived local daemon that writes spend from observed state)
// and prism-research-portal (a stateless Lambda that reads spend written by an external collector).
//
// # The model (one conserved quantity, moved in time)
//
//	remaining_principal(t) = Σ funding_sources_active(t) − cumulative_spend(t)   ≥ 0
//
// remaining_principal is real money, hard-floored at zero. Banking and borrowing move spend in TIME,
// never in amount: banking pushes spend later (idle now → burst later), borrowing pulls your own
// remaining budget earlier (spend ahead of pace, repaid by idling). You never go below $0; hitting
// zero means done, unless a new funding source is added.
//
// State is event-sourced from two ordered logs, so it is derived and reproducible — two hosts that
// fold the same logs compute identical state:
//
//   - SpendEvent — actual, already-attributed cost line items (the actuals ledger).
//   - PlanEvent  — plan mutations (source added/expired, window extended, allocation changed, freeze).
//
// # Two interaction shapes
//
//   - CheckLaunch(scope, cost) → Decision{Allow|Warn|Block} — synchronous, budget-only pre-flight.
//   - ActionSink.OnState(scope, BurnState)                   — reactive push after spend moves.
//
// # Three orthogonal policy axes (the flexibility surface)
//
//   - Sourcing   — how dated sources combine and drain (ExpiryFirst is the v0 built-in).
//   - Pacing     — how idle allowance is treated (BankAndReserve vs. RateAdjust).
//   - Projection — which variable is pinned on deviation: fixed-date/floating-rate
//     (RateTargetPolicy) vs. fixed-rate/floating-date (DeadlineFloatPolicy).
//
// The authoritative design record is prism/docs/architecture/BUDGET_ENGINE_ARCHITECTURE.md.
package budgetengine
