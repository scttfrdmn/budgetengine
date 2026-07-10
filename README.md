# budgetengine

A standalone, host-agnostic budget engine for cloud spend against time-boxed, possibly multi-source
funding. **Zero dependencies outside the Go standard library** — that is the enforceable proof of its
host-agnostic boundary. Two very different hosts embed it unchanged: a long-lived daemon that *writes*
spend (Prism) and a stateless Lambda that *reads* spend written by an external collector
(prism-research-portal).

## The model — one conserved quantity, moved in time

```
remaining_principal(t) = Σ funding_sources_active(t) − cumulative_spend(t)   ≥ 0
```

`remaining_principal` is real money, hard-floored at zero. **Banking and borrowing move spend in
_time_, never in _amount_:** banking pushes spend later (idle now → burst later); borrowing pulls
your own remaining budget earlier (spend ahead of pace, repaid by idling). You never go below `$0`;
hitting zero means done, unless a new funding source is added.

State is **event-sourced** from two ordered logs, so it is derived and reproducible — two hosts that
fold the same logs compute identical state:

- **`SpendEvent`** — actual, already-attributed cost line items (the actuals ledger). Carries the
  authoritative `Amount` delta plus optional line-item metadata (`ResourceID`, `Compute`/`Storage`/
  `Network`) for per-resource breakdown; the fold uses only `Amount`, so the metadata is additive and
  backward-compatible.
- **`PlanEvent`** — plan mutations (source added/expired, window extended, allocation changed, freeze).

The nominal capacity curve is piecewise-constant and may be disjoint (a funding gap → rate 0);
`available_to_date` is its exact finite integral to `now`. `pace_deviation = available_to_date −
spent` (positive = banked, negative = borrowed). Plan edits are causal / go-forward-only: a source
added today never rewrites yesterday's curve or forgives already-accrued bank/borrow.

## Two interaction shapes

- **`Engine.CheckLaunch(scope, allocID, cost) → Decision{Allow|Warn|Block}`** — synchronous,
  budget-only pre-flight. (Non-budget preconditions stay host-side.)
- **`Engine.Evaluate(...) → BurnState`** + **`ActionSink.OnState`** — reactive push after spend moves.

## Three orthogonal policy axes

| Axis | v0 built-ins | Role |
|------|-------------|------|
| Sourcing | `ExpiryFirst` | how dated sources combine into the nominal rate + drain order |
| Pacing | `BankAndReserve`, `RateAdjust` | how idle allowance is treated (reserve vs. memoryless) |
| Projection | `RateTargetPolicy` (fixed-date), `DeadlineFloatPolicy` (fixed-rate) | which variable is pinned on deviation |

Compose freely: a multi-month grant is `ExpiryFirst × BankAndReserve × RateTargetPolicy`; a monthly
cloud budget is single-source `× RateAdjust × DeadlineFloatPolicy`.

## Ports (host-supplied)

Read (both hosts): `SpendSource`, `PlanSource`. Write (appending host only): `SpendWriter`,
`PlanWriter`. Plus `ActionSink` (reactive enforcement) and `Clock`.

## Status

**v0.** Exposes the fold, `Evaluate`, `CheckLaunch`, all record/port types, and one-or-two built-ins
per policy axis — enough to run every worked scenario in the design. **Defers:** folded-state
checkpoint caching (log is authoritative), priority/proportional sourcing, multi-allocation carving
of a shared pool (v0 treats an allocation as an absolute cap over the pool; single-allocation is
exact), and any persistence adapter (host territory).

Design record: `prism/docs/architecture/BUDGET_ENGINE_ARCHITECTURE.md`.
