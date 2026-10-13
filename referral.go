package affiliate

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Referral is the lifelong link of a user to the affiliate who referred them.
//
// It is written once, when the user signs up with a referral code, and never
// changes: the cookie that carried the code only decides who the referrer is
// at that moment. Every payment of the user afterwards -- the first purchase
// and every renewal -- pays a commission up the tree that starts here.
type Referral struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	// UserID is the user who was referred, unique per tenant: a user has one
	// referrer, for life.
	UserID string `db:"user_id"`

	// AffiliateID is the affiliate who referred them.
	AffiliateID string `db:"affiliate_id"`

	// Document is the user's CPF or CNPJ when the application knows it, digits
	// only and unique per tenant when present, so one person is not referred
	// twice under two accounts.
	Document OptionalText `db:"document"`

	// Code is the code the user signed up with.
	Code string `db:"code"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

var referralTable = model.NewTable(model.TableSpec{
	Name:      referralsTable,
	New:       func() model.Entity { return new(Referral) },
	ManualKey: true,
})

// Click is one opening of a referral link. The address is kept only as a
// hash, with the tenant in it, so a click can be told apart from another and
// nobody's address is stored.
type Click struct {
	model.Model

	ID          string    `db:"id"`
	TenantID    string    `db:"tenant_id"`
	AffiliateID string    `db:"affiliate_id"`
	At          time.Time `db:"at"`
	LandingURL  string    `db:"landing_url"`
	IPHash      string    `db:"ip_hash"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

var clickTable = model.NewTable(model.TableSpec{
	Name:      clicksTable,
	New:       func() model.Entity { return new(Click) },
	ManualKey: true,
})
