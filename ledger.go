package affiliate

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Conversion is one payment of a referred user -- a first purchase or a
// renewal -- as the application reported it. It is recorded once per order
// reference, and it is what the commissions of every level point at.
type Conversion struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	// OrderRef is the application's identifier of the payment, unique per
	// tenant: reporting the same payment twice records it once.
	OrderRef string `db:"order_ref"`

	BuyerUserID string `db:"buyer_user_id"`
	ProductID   string `db:"product_id"`

	// Amount is the payment in centavos, what the rates apply to.
	Amount  int64 `db:"amount"`
	Renewal Flag  `db:"renewal"`

	// AffiliateID is the buyer's referrer, the first level, empty when the
	// buyer was referred by nobody. PlanID is the plan that paid it, empty
	// when no plan was active.
	AffiliateID string `db:"affiliate_id"`
	PlanID      string `db:"plan_id"`

	Status     ConversionStatus `db:"status"`
	OccurredAt time.Time        `db:"occurred_at"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// ConversionStatus is whether a payment still stands.
type ConversionStatus string

const (
	// ConversionCounted stands, and its commissions with it.
	ConversionCounted ConversionStatus = "counted"
	// ConversionRefunded was given back, and its commissions reversed.
	ConversionRefunded ConversionStatus = "refunded"
)

var conversionTable = model.NewTable(model.TableSpec{
	Name:      conversionsTable,
	New:       func() model.Entity { return new(Conversion) },
	ManualKey: true,
})

// Commission is what one level of the tree earned on one conversion. It is
// deposited in the affiliate's wallet the moment it is created -- no
// waiting period -- and stays there until they withdraw it. A refund, or the
// admin, reverses it: the same amount leaves the wallet again, and the
// balance may go below zero, which blocks withdrawals until later
// commissions, or the admin, bring it back.
type Commission struct {
	model.Model

	ID           string `db:"id"`
	TenantID     string `db:"tenant_id"`
	ConversionID string `db:"conversion_id"`
	AffiliateID  string `db:"affiliate_id"`

	Level   int   `db:"level"`
	RateBps int64 `db:"rate_bps"`
	Amount  int64 `db:"amount"`

	Status CommissionStatus `db:"status"`

	// WalletOperationID is the deposit; ReversalOperationID the withdrawal
	// that undid it.
	WalletOperationID   string `db:"wallet_operation_id"`
	ReversalOperationID string `db:"reversal_operation_id"`
	ReversedReason      string `db:"reversed_reason"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// CommissionStatus is where a commission stands.
type CommissionStatus string

const (
	// CommissionAvailable is in the wallet.
	CommissionAvailable CommissionStatus = "available"
	// CommissionReversed was taken back out.
	CommissionReversed CommissionStatus = "reversed"
)

var commissionTable = model.NewTable(model.TableSpec{
	Name:      commissionsTable,
	New:       func() model.Entity { return new(Commission) },
	ManualKey: true,
})

// WithdrawalDay is the one withdrawal an affiliate asked for on a day. Its
// unique index is what holds the rule of one a day under two requests sent at
// once; a request of the day that was rejected or cancelled frees the day.
type WithdrawalDay struct {
	model.Model

	ID                  string    `db:"id"`
	TenantID            string    `db:"tenant_id"`
	AffiliateID         string    `db:"affiliate_id"`
	Day                 Day       `db:"day"`
	WithdrawalRequestID string    `db:"withdrawal_request_id"`
	Attempt             int       `db:"attempt"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

var withdrawalDayTable = model.NewTable(model.TableSpec{
	Name:      withdrawalDaysTable,
	New:       func() model.Entity { return new(WithdrawalDay) },
	ManualKey: true,
})
