package affiliate

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Plan is how commissions are paid: how many levels up the tree, at which
// rate each, how long a referral cookie lives, and the limits of a
// withdrawal. The admin defines plans; one is active at a time, and a
// conversion is paid by the plan active when it arrived.
type Plan struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	Name string   `db:"name"`
	Kind PlanKind `db:"kind"`

	// CookieDays is how long the referral cookie set by /r/{code} lives.
	CookieDays int `db:"cookie_days"`

	// MinWithdrawal and MaxWithdrawal are centavos. A max of zero is no max.
	MinWithdrawal int64 `db:"min_withdrawal"`
	MaxWithdrawal int64 `db:"max_withdrawal"`

	Active Flag `db:"active"`

	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// PlanKind is how far up the tree a plan pays.
type PlanKind string

const (
	// Linear pays the referrer alone: one level.
	Linear PlanKind = "linear"
	// Multilevel pays up to MaxLevels levels up the tree.
	Multilevel PlanKind = "multilevel"
)

// MaxLevels is the deepest level a plan pays.
const MaxLevels = 4

var planTable = model.NewTable(model.TableSpec{
	Name:      plansTable,
	New:       func() model.Entity { return new(Plan) },
	ManualKey: true,
})

// PlanLevel is the rate one level of a plan pays, in basis points of the
// payment: 150 is 1,5%.
type PlanLevel struct {
	model.Model

	ID        string    `db:"id"`
	TenantID  string    `db:"tenant_id"`
	PlanID    string    `db:"plan_id"`
	Level     int       `db:"level"`
	RateBps   int64     `db:"rate_bps"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

var planLevelTable = model.NewTable(model.TableSpec{
	Name:      planLevelsTable,
	New:       func() model.Entity { return new(PlanLevel) },
	ManualKey: true,
})
