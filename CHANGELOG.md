# Changelog

All notable changes to budgetengine are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0]

### Changed
- **`BankAndReserve` pacing now genuinely differs from `RateAdjust`.** Previously the two policies
  computed an identical forward rate (`RemainingPrincipal / remaining_time`), so choosing between them
  was a no-op. `BankAndReserve` now **holds the nominal (planned) forward rate** — the funding curve
  integrated over `(now, window.End]` — capped by solvency (`min(nominalForward, remaining/rem)`).
  Under-spending therefore banks (the rate stays at plan) instead of inflating the re-paced rate,
  which is the behavior a multi-month grant needs to sustain a later burst. `RateAdjust` keeps its
  memoryless re-pacing semantics. The two coincide when spend has tracked the plan, or when the
  allocation carries no capacity curve (degenerate/parity plans), in which case `BankAndReserve` falls
  back to the solvency rate.
- This is a **pure projection-rate refinement**: `Fold`, `CheckLaunch`, `AvailableToDate`, and
  `PaceDeviation` are unchanged, so launch decisions and banked-headroom accounting are unaffected.
  Only `BurnState.SustainableRate` (and transitively `ProjectedZeroDate`) changes under
  `BankAndReserve`.

### Added
- **`CapacityCurve.AvailableBetween(from, to time.Time) float64`** — integrates the piecewise-constant
  nominal capacity curve over an arbitrary span. `AvailableToDate(now)` is the special case
  `AvailableBetween(curveStart, now)`; `BankAndReserve` uses it to read the go-forward nominal rate.

## [0.2.0]

### Added
- Optional line-item fields on `SpendEvent` (`ResourceID`, `Compute`, `Storage`, `Network`) —
  additive and backward-compatible; the fold and `CheckLaunch` use only `Amount`.

## [0.1.0]

### Added
- Initial standalone, host-agnostic budget engine: event-sourced fold (spend + plan logs), three
  orthogonal policy axes (sourcing / pacing / projection), `CheckLaunch` pre-flight decision, and
  `Evaluate` → `BurnState` reactive readout.
