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

	// MaxMonths and MaxPayments end the commission of a referred buyer: a
	// payment made MaxMonths calendar months after the buyer's first
	// commissionable payment, or after MaxPayments commissioned payments, is
	// recorded and pays nothing. Zero is no limit.
	MaxMonths   int `db:"max_months"`
	MaxPayments int `db:"max_payments"`

	// AnnualBaseBps is the share of an annual payment the rates apply to, in
	// basis points. 10000, the value of a plan that never set it, is the
	// whole payment.
	AnnualBaseBps int64 `db:"annual_base_bps"`

	// GraceDays is how many days after the payment a commission stays
	// pending before it reaches the wallet. Zero deposits it at once.
	GraceDays int `db:"grace_days"`

	// PriorPayerMonths is the look-back, in months, in which a buyer who had
	// already paid earns the affiliate nothing. Zero turns the rule off.
	PriorPayerMonths int `db:"prior_payer_months"`

	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// FullBaseBps is the whole payment, in basis points: the value of
// Plan.AnnualBaseBps that changes nothing.
const FullBaseBps = 10000

// AnnualBase is the share of an annual payment the rates apply to, in basis
// points, FullBaseBps for a plan that never set one.
func (p Plan) AnnualBase() int64 {
	if p.AnnualBaseBps <= 0 || p.AnnualBaseBps > FullBaseBps {
		return FullBaseBps
	}
	return p.AnnualBaseBps
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
