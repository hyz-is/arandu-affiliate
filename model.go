package affiliate

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"

	"github.com/arandu-io/hesape/database/model"
	br "github.com/hyz-is/arandu-br"
)

// Affiliate is somebody who refers people to the application and earns a
// commission on every payment those people make, and on the payments of the
// people they referred in turn, up to the levels the plan pays.
//
// An affiliate is a login user with a code. The code is what the referral
// link carries -- /r/{code} -- and it is unique in its tenant. The document
// is the affiliate's CPF or CNPJ: it is what keeps one person from appearing
// twice in a referral tree, and the name every payout account of theirs has
// to be in.
type Affiliate struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`

	// UserID is the login user who signs in to the panel, unique per tenant.
	UserID string `db:"user_id"`

	// Code is what the referral link carries, unique per tenant.
	Code string `db:"code"`

	// Document is the CPF or the CNPJ, digits only, unique per tenant.
	Document string `db:"document"`

	// Name is how the affiliate is called on the screens and on a payout.
	Name string `db:"name"`

	// Status is whether the affiliate earns and withdraws. A blocked
	// affiliate keeps their history and their place in the tree; their level
	// is skipped when a commission is paid, and the levels above them still
	// earn.
	Status AffiliateStatus `db:"status"`

	CreatedBy string    `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// AffiliateStatus is whether an affiliate earns and withdraws.
type AffiliateStatus string

const (
	// AffiliateActive earns commissions and withdraws them.
	AffiliateActive AffiliateStatus = "active"
	// AffiliateBlocked earns nothing new and cannot withdraw.
	AffiliateBlocked AffiliateStatus = "blocked"
)

// IsActive reports whether the affiliate earns and withdraws.
func (a Affiliate) IsActive() bool { return a.Status == AffiliateActive }

var affiliateTable = model.NewTable(model.TableSpec{
	Name:      affiliatesTable,
	New:       func() model.Entity { return new(Affiliate) },
	ManualKey: true,
})

// arandu:begin custom

// The table names, declared once: the migrations and the queries read them
// from here, so a rename is one edit.
const (
	affiliatesTable     = "affiliates"
	referralsTable      = "affiliate_referrals"
	plansTable          = "affiliate_plans"
	planLevelsTable     = "affiliate_plan_levels"
	clicksTable         = "affiliate_clicks"
	conversionsTable    = "affiliate_conversions"
	commissionsTable    = "affiliate_commissions"
	withdrawalDaysTable = "affiliate_withdrawal_days"
)

// Column widths, declared once for the migrations and the validation, so a
// value the form accepts is a value the column holds.
const (
	maxIdentifierLen = 64
	maxCodeLen       = 32
	maxNameLen       = 160
	maxDocumentLen   = 14
	maxShortLen      = 40
	maxURLLen        = 512
	maxTextLen       = 1000
)

// ErrNotFound is a record the caller may not see or that does not exist. It
// wraps model.ErrModelNotFound, which the router answers with 404.
var ErrNotFound = fmt.Errorf("affiliate: record not found: %w", model.ErrModelNotFound)

// normalizeDocument checks a CPF or a CNPJ and returns it as it is stored. Its
// length says which it is: eleven digits are a CPF, fourteen characters a
// CNPJ. The check digits are arandu-br's.
func normalizeDocument(document string) (string, error) {
	digits := 0
	for _, r := range document {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			digits++
		}
	}
	switch digits {
	case 11:
		return br.ParseCPF(document)
	case 14:
		return br.ParseCNPJ(document)
	}
	return "", fmt.Errorf("affiliate: %q is neither a CPF nor a CNPJ", document)
}

// Day is a calendar day, as YYYY-MM-DD, in the application's timezone.
type Day string

// DayLayout is how a Day is written.
const DayLayout = "2006-01-02"

// ParseDay reads a day, and the empty string as no day.
func ParseDay(s string) (Day, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(DayLayout, s)
	if err != nil {
		return "", fmt.Errorf("affiliate: %q is not a day in the form %s", s, DayLayout)
	}
	return Day(t.Format(DayLayout)), nil
}

// DayOf is the calendar day of an instant in a timezone.
func DayOf(t time.Time, loc *time.Location) Day {
	return Day(t.In(loc).Format(DayLayout))
}

// IsZero reports the empty day.
func (d Day) IsZero() bool { return d == "" }

// Time is midnight of the day, in UTC.
func (d Day) Time() time.Time {
	t, _ := time.Parse(DayLayout, string(d))
	return t
}

// Flag is a yes-or-no stored as 0 or 1, which every engine reads the same.
type Flag bool

// Value writes the flag as 0 or 1.
func (f Flag) Value() (driver.Value, error) {
	if f {
		return int64(1), nil
	}
	return int64(0), nil
}

// Scan reads a flag from whatever the engine answers.
func (f *Flag) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*f = false
	case bool:
		*f = Flag(v)
	case int64:
		*f = v != 0
	case int32:
		*f = v != 0
	case int:
		*f = v != 0
	case float64:
		*f = v != 0
	case []byte:
		return f.Scan(string(v))
	case string:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("affiliate: %q is not a yes-or-no", v)
		}
		*f = Flag(b)
	default:
		return fmt.Errorf("affiliate: cannot read %T as a yes-or-no", value)
	}
	return nil
}

// OptionalText is a text column that is NULL when empty, so a unique index
// over it holds among the rows that have a value and lets every row without
// one through: two users whose document nobody knows are not the same person.
type OptionalText string

// Value writes the empty text as NULL.
func (t OptionalText) Value() (driver.Value, error) {
	if t == "" {
		return nil, nil
	}
	return string(t), nil
}

// Scan reads NULL as the empty text.
func (t *OptionalText) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*t = ""
	case string:
		*t = OptionalText(v)
	case []byte:
		*t = OptionalText(string(v))
	default:
		return fmt.Errorf("affiliate: cannot read %T as text", value)
	}
	return nil
}

// cents formats centavos as the decimal text arandu-wallet takes: 12345 is
// "123.45", -5 is "-0.05".
func cents(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// arandu:end custom
