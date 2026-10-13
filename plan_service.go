package affiliate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
)

// PlanRequest is what defining a commission plan takes.
type PlanRequest struct {
	Name string
	Kind PlanKind
	// Rates are the basis points of each level, the first level first: one
	// for a linear plan, one to MaxLevels for a multilevel one.
	Rates []int64
	// CookieDays is how long the referral cookie lives. Zero means
	// DefaultCookieDays.
	CookieDays int
	// MinWithdrawal and MaxWithdrawal are centavos; a max of zero is no max.
	MinWithdrawal int64
	MaxWithdrawal int64
}

// Validate reports the errors per field.
func (r PlanRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "name", r.Name)
	validation.MaxLen(e, "name", r.Name, maxNameLen)
	switch r.Kind {
	case Linear:
		if len(r.Rates) != 1 {
			e.Add("rates", "a linear plan pays one level")
		}
	case Multilevel:
		if len(r.Rates) < 1 || len(r.Rates) > MaxLevels {
			e.Add("rates", fmt.Sprintf("a multilevel plan pays 1 to %d levels", MaxLevels))
		}
	default:
		e.Add("kind", fmt.Sprintf("must be %q or %q", Linear, Multilevel))
	}
	var total int64
	for _, rate := range r.Rates {
		if rate <= 0 || rate > 10000 {
			e.Add("rates", "every level's rate is above 0 and at most 100%")
			break
		}
		total += rate
	}
	if total > 10000 {
		e.Add("rates", "the levels together cannot pay more than 100% of a payment")
	}
	if r.CookieDays < 0 || r.CookieDays > 365 {
		e.Add("cookie_days", "is between 1 and 365 days")
	}
	if r.MinWithdrawal < 0 {
		e.Add("min_withdrawal", "cannot be negative")
	}
	if r.MaxWithdrawal < 0 || (r.MaxWithdrawal > 0 && r.MaxWithdrawal < r.MinWithdrawal) {
		e.Add("max_withdrawal", "is zero, for no limit, or at least the minimum")
	}
	return e
}

var _ validation.Validatable = PlanRequest{}

// PlanView is a plan with its levels, the first level first.
type PlanView struct {
	Plan   Plan
	Levels []PlanLevel
}

// Rate is the basis points a level pays, zero for a level the plan does not
// pay.
func (p PlanView) Rate(level int) int64 {
	for _, l := range p.Levels {
		if l.Level == level {
			return l.RateBps
		}
	}
	return 0
}

// CreatePlan defines a plan, and makes it the active one when activate is
// set. A plan is never edited: a new one is defined and activated, and every
// commission keeps the plan that paid it.
func (s *AffiliateService) CreatePlan(ctx context.Context, actor security.Subject, in PlanRequest, activate bool) (*PlanView, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, PlansManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	plan, err := Plans(s.db).New()
	if err != nil {
		return nil, err
	}
	if plan.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	plan.TenantID = data.Tenant(g)
	plan.Name, plan.Kind = strings.TrimSpace(in.Name), in.Kind
	plan.CookieDays = in.CookieDays
	if plan.CookieDays == 0 {
		plan.CookieDays = DefaultCookieDays
	}
	plan.MinWithdrawal, plan.MaxWithdrawal = in.MinWithdrawal, in.MaxWithdrawal
	plan.Active = Flag(activate)
	plan.CreatedBy, plan.CreatedAt, plan.UpdatedAt = actor.ID, now, now

	view := &PlanView{}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if activate {
			if err := s.deactivateAll(ctx, g); err != nil {
				return err
			}
		}
		if _, err := plan.Save(ctx, g); err != nil {
			return err
		}
		for i, rate := range in.Rates {
			level, err := PlanLevels(s.db).New()
			if err != nil {
				return err
			}
			if level.ID, err = data.NewID(); err != nil {
				return err
			}
			level.TenantID, level.PlanID, level.Level, level.RateBps = data.Tenant(g), plan.ID, i+1, rate
			level.CreatedAt, level.UpdatedAt = now, now
			if _, err := level.Save(ctx, g); err != nil {
				return err
			}
			view.Levels = append(view.Levels, *level)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view.Plan = *plan
	return view, nil
}

// deactivateAll turns every active plan off, inside the caller's transaction.
func (s *AffiliateService) deactivateAll(ctx context.Context, g security.Grant) error {
	active, err := Plans(s.db).Where("active", "=", 1).Get(ctx, g)
	if err != nil {
		return err
	}
	for _, p := range active {
		p.Active, p.UpdatedAt = false, s.now()
		if _, err := p.Save(ctx, g); err != nil {
			return err
		}
	}
	return nil
}

// ActivatePlan makes a plan the one new conversions are paid by.
func (s *AffiliateService) ActivatePlan(ctx context.Context, actor security.Subject, id string) (*PlanView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, PlansManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	plan, err := Plans(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, ErrNotFound
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if err := s.deactivateAll(ctx, g); err != nil {
			return err
		}
		plan.Active, plan.UpdatedAt = true, s.now()
		_, err := plan.Save(ctx, g)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.viewOf(ctx, g, plan)
}

// Plans lists every plan, the active one first, then the newest.
func (s *AffiliateService) Plans(ctx context.Context, actor security.Subject) ([]PlanView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, PlansManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	plans, err := Plans(s.db).OrderByDesc("active").OrderByDesc("created_at").OrderByDesc("id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	levels, err := PlanLevels(s.db).OrderBy("level").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	byPlan := map[string][]PlanLevel{}
	for _, l := range levels {
		byPlan[l.PlanID] = append(byPlan[l.PlanID], *l)
	}
	out := make([]PlanView, 0, len(plans))
	for _, p := range plans {
		out = append(out, PlanView{Plan: *p, Levels: byPlan[p.ID]})
	}
	return out, nil
}

// ActivePlan is the plan in force, for anybody who reads a panel: an
// affiliate consults it, never changes it. Nil when no plan is active.
func (s *AffiliateService) ActivePlan(ctx context.Context, actor security.Subject) (*PlanView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, err
	}
	plan, err := Plans(s.db).Where("active", "=", 1).OrderByDesc("updated_at").First(ctx, g)
	if err != nil || plan == nil {
		return nil, err
	}
	return s.viewOf(ctx, g, plan)
}

// activePlan is the plan in force, after the caller authorized.
func (s *AffiliateService) activePlan(ctx context.Context, g security.Grant) (*PlanView, error) {
	plan, err := Plans(s.db).Where("active", "=", 1).OrderByDesc("updated_at").First(ctx, g)
	if err != nil || plan == nil {
		return nil, err
	}
	return s.viewOf(ctx, g, plan)
}

// viewOf loads a plan's levels.
func (s *AffiliateService) viewOf(ctx context.Context, g security.Grant, plan *Plan) (*PlanView, error) {
	levels, err := PlanLevels(s.db).Where("plan_id", "=", plan.ID).OrderBy("level").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	view := &PlanView{Plan: *plan}
	for _, l := range levels {
		view.Levels = append(view.Levels, *l)
	}
	sort.Slice(view.Levels, func(i, j int) bool { return view.Levels[i].Level < view.Levels[j].Level })
	return view, nil
}

// limits are the withdrawal limits and the cookie of the plan in force, or
// the defaults while none is.
func (s *AffiliateService) limits(plan *PlanView) (min, max int64, cookieDays int) {
	if plan == nil {
		return s.minWithdrawal, 0, s.cookieDays
	}
	return plan.Plan.MinWithdrawal, plan.Plan.MaxWithdrawal, plan.Plan.CookieDays
}
