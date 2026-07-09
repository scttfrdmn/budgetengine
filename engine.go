package budgetengine

import (
	"context"
	"time"
)

// The three policy axes. Interfaces are declared here (in the core package) so Engine depends on
// local types; concrete implementations live in the policy subpackage (which imports core). This
// keeps the composition one-directional and cycle-free.

// SourcingPolicy shapes how dated sources combine into the nominal capacity rate for a segment, and
// the order in which sources drain. Called during the fold, once per capacity segment.
type SourcingPolicy interface {
	// NominalRate returns the nominal USD/second for a segment given the sources active in it.
	NominalRate(active []FundingSource, seg Window) float64
	// DrainOrder ranks active sources for depletion (index 0 drains first).
	DrainOrder(active []FundingSource, at time.Time) []FundingSource
}

// PacingPolicy governs the forward sustainable rate — how idle allowance is treated. It does not
// change the instantaneous ceiling (that is the paced available_to_date); it changes the rate the
// engine reports going forward.
type PacingPolicy interface {
	// SustainableRate returns the go-forward USD/second the allocation may sustain.
	SustainableRate(as AllocationState, window Window, now time.Time) float64
}

// ProjectionPolicy decides which readout drives OnTrack / enforcement. Both readouts are always
// computed; the policy picks the authoritative one.
type ProjectionPolicy interface {
	OnTrack(as AllocationState, window Window, now time.Time) bool
}

// BurnState is the reactive-path output: a full picture of an allocation's budget health.
type BurnState struct {
	Scope              Scope
	AllocationID       string
	RemainingPrincipal float64   // ≥ 0, real money (solvency)
	AvailableToDate    float64   // paced ceiling
	PaceDeviation      float64   // signed: + banked/under-pace, − borrowed/ahead
	SustainableRate    float64   // USD/second, per PacingPolicy (fixed-date view)
	ProjectedZeroDate  time.Time // fixed-rate view: when remaining hits zero at recent rate
	Solvent            bool      // RemainingPrincipal > 0
	OnTrack            bool      // per active ProjectionPolicy
}

// Engine composes the read ports, a clock, the three policies, and (optionally) an ActionSink.
// Construct with New.
type Engine struct {
	spend         SpendSource
	plan          PlanSource
	clock         Clock
	sourcing      SourcingPolicy
	pacing        PacingPolicy
	projection    ProjectionPolicy
	sink          ActionSink
	warnThreshold float64 // fraction of ceiling that triggers Warn; default 0.80
}

// Option configures an Engine.
type Option func(*Engine)

// WithSourcing sets the sourcing policy.
func WithSourcing(p SourcingPolicy) Option { return func(e *Engine) { e.sourcing = p } }

// WithPacing sets the pacing policy.
func WithPacing(p PacingPolicy) Option { return func(e *Engine) { e.pacing = p } }

// WithProjection sets the projection policy.
func WithProjection(p ProjectionPolicy) Option { return func(e *Engine) { e.projection = p } }

// WithActionSink sets the reactive enforcement sink.
func WithActionSink(s ActionSink) Option { return func(e *Engine) { e.sink = s } }

// WithWarnThreshold overrides the warn fraction (0,1).
func WithWarnThreshold(f float64) Option { return func(e *Engine) { e.warnThreshold = f } }

// New builds an Engine over the given read ports and clock. Policies default to the multi-month
// grant composition (expiry-first × bank-and-reserve × fixed-date) unless overridden; the caller
// supplies the concrete defaults via options (the core package ships no policy impls to stay
// cycle-free). warnThreshold defaults to 0.80.
func New(spend SpendSource, plan PlanSource, clock Clock, opts ...Option) *Engine {
	e := &Engine{spend: spend, plan: plan, clock: clock, warnThreshold: 0.80}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Engine) warn() float64 {
	if e.warnThreshold > 0 && e.warnThreshold < 1 {
		return e.warnThreshold
	}
	return 0.80
}

// Evaluate folds the logs and reports the allocation's BurnState at now. Read-only; uses only the
// read ports + clock + the three policies.
func (e *Engine) Evaluate(ctx context.Context, scope Scope, allocID string) (BurnState, error) {
	now := e.clock.Now()
	planLog, err := e.plan.Plan(ctx, scope)
	if err != nil {
		return BurnState{}, err
	}
	spendLog, err := e.spend.Spend(ctx, scope)
	if err != nil {
		return BurnState{}, err
	}
	st := Fold(planLog, spendLog, now, e.sourcing)
	as := st.Allocs[allocID]
	if as == nil {
		as = &AllocationState{}
	}

	rate := e.pacing.SustainableRate(*as, st.Window, now)
	bs := BurnState{
		Scope:              scope,
		AllocationID:       allocID,
		RemainingPrincipal: as.RemainingPrincipal,
		AvailableToDate:    as.AvailableToDate,
		PaceDeviation:      as.PaceDeviation,
		SustainableRate:    rate,
		ProjectedZeroDate:  projectedZero(now, as.RemainingPrincipal, rate),
		Solvent:            as.RemainingPrincipal > 0,
		OnTrack:            e.projection.OnTrack(*as, st.Window, now),
	}
	return bs, nil
}

// Notify folds current state and pushes it to the ActionSink (the reactive path). No-op if no sink
// was configured. Hosts call this after appending spend.
func (e *Engine) Notify(ctx context.Context, scope Scope, allocID string) error {
	if e.sink == nil {
		return nil
	}
	bs, err := e.Evaluate(ctx, scope, allocID)
	if err != nil {
		return err
	}
	return e.sink.OnState(ctx, scope, bs)
}

// projectedZero is the fixed-rate view: now + remaining/rate. Zero time when rate ≤ 0 (never, or
// already at zero).
func projectedZero(now time.Time, remaining, rate float64) time.Time {
	if rate <= 0 || remaining <= 0 {
		return time.Time{}
	}
	secs := remaining / rate
	return now.Add(time.Duration(secs * float64(time.Second)))
}
