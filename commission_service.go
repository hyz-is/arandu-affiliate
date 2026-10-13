package affiliate

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	wallet "github.com/hyz-is/arandu-wallet"
)

// WalletSlug is the slug of the affiliate's wallet in arandu-wallet.
const WalletSlug = "affiliate"

// PayoutVia is the name this package asks for its affiliates' withdrawals
// under, and the one their wallets are opened with: arandu-wallet refuses a
// request for those wallets that does not carry it, so the minimum, the
// maximum and one request a day cannot be stepped around.
const PayoutVia = "affiliate"

// ConversionRequest is a payment of a user, as the application reports it.
type ConversionRequest struct {
	// OrderRef is the application's identifier of the payment, unique per
	// tenant: reporting it twice records it once.
	OrderRef    string
	BuyerUserID string
	ProductID   string
	// Amount is the payment in centavos.
	Amount int64
	// Renewal is a payment of something the buyer already had. It pays
	// commissions like a first purchase: they are for life.
	Renewal    bool
	OccurredAt time.Time
}

// Validate reports the errors per field.
func (r ConversionRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "order_ref", r.OrderRef)
	validation.MaxLen(e, "order_ref", r.OrderRef, maxIdentifierLen*2)
	validation.Required(e, "buyer_user_id", r.BuyerUserID)
	validation.MaxLen(e, "buyer_user_id", r.BuyerUserID, maxIdentifierLen)
	validation.MaxLen(e, "product_id", r.ProductID, maxIdentifierLen)
	if r.Amount <= 0 {
		e.Add("amount", "is a payment above zero, in centavos")
	}
	if r.OccurredAt.IsZero() {
		e.Add("occurred_at", "is required")
	}
	return e
}

var _ validation.Validatable = ConversionRequest{}

// roundDiv divides rounding half away from zero.
func roundDiv(n, d int64) int64 {
	if (n < 0) != (d < 0) {
		return -((abs(n) + abs(d)/2) / abs(d))
	}
	return (abs(n) + abs(d)/2) / abs(d)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// CommissionOf is what a rate in basis points pays on an amount in centavos,
// rounded half up to the centavo.
func CommissionOf(amount, rateBps int64) int64 { return roundDiv(amount*rateBps, 10000) }

// RecordConversion records a payment of a user and pays every level of the
// tree above them, by the plan active now: the buyer's referrer is level one,
// the referrer's referrer level two, and so on. A blocked affiliate's level
// is skipped and the levels above still earn. Each commission is deposited in
// the affiliate's wallet at once.
//
// It is idempotent by OrderRef: a payment reported again answers what the
// first report recorded. A buyer with no referrer, or a payment with no plan
// active, is recorded and pays nothing.
func (s *AffiliateService) RecordConversion(ctx context.Context, actor security.Subject, in ConversionRequest) (*Conversion, []*Commission, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, nil, err
	}
	ref := strings.TrimSpace(in.OrderRef)
	if existing, err := Conversions(s.db).Where("order_ref", "=", ref).First(ctx, g); err != nil || existing != nil {
		if err != nil {
			return nil, nil, err
		}
		paid, err := Commissions(s.db).Where("conversion_id", "=", existing.ID).OrderBy("level").Get(ctx, g)
		return existing, paid, err
	}

	plan, err := s.activePlan(ctx, g)
	if err != nil {
		return nil, nil, err
	}
	conversion, err := Conversions(s.db).New()
	if err != nil {
		return nil, nil, err
	}
	if conversion.ID, err = data.NewID(); err != nil {
		return nil, nil, err
	}
	now := s.now()
	conversion.TenantID, conversion.OrderRef = data.Tenant(g), ref
	conversion.BuyerUserID, conversion.ProductID = strings.TrimSpace(in.BuyerUserID), strings.TrimSpace(in.ProductID)
	conversion.Amount, conversion.Renewal = in.Amount, Flag(in.Renewal)
	conversion.Status, conversion.OccurredAt = ConversionCounted, in.OccurredAt.UTC()
	conversion.CreatedAt, conversion.UpdatedAt = now, now
	if plan != nil {
		conversion.PlanID = plan.Plan.ID
	}

	// Who earns: up the tree from the buyer, one level per referral, as far
	// as the plan pays.
	type earner struct {
		affiliate *Affiliate
		level     int
		rate      int64
	}
	var earners []earner
	above, err := Referrals(s.db).Where("user_id", "=", conversion.BuyerUserID).First(ctx, g)
	if err != nil {
		return nil, nil, err
	}
	for level := 1; above != nil && level <= MaxLevels; level++ {
		a, err := Affiliates(s.db).WhereKey(above.AffiliateID).First(ctx, g)
		if err != nil {
			return nil, nil, err
		}
		if a == nil {
			break
		}
		if level == 1 {
			conversion.AffiliateID = a.ID
		}
		if plan != nil && a.IsActive() {
			if rate := plan.Rate(level); rate > 0 {
				earners = append(earners, earner{affiliate: a, level: level, rate: rate})
			}
		}
		if above, err = Referrals(s.db).Where("user_id", "=", a.UserID).First(ctx, g); err != nil {
			return nil, nil, err
		}
	}

	var paid []*Commission
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if _, err := conversion.Save(ctx, g); err != nil {
			return err
		}
		for _, e := range earners {
			amount := CommissionOf(conversion.Amount, e.rate)
			if amount <= 0 {
				continue
			}
			c, err := Commissions(s.db).New()
			if err != nil {
				return err
			}
			if c.ID, err = data.NewID(); err != nil {
				return err
			}
			c.TenantID, c.ConversionID, c.AffiliateID = data.Tenant(g), conversion.ID, e.affiliate.ID
			c.Level, c.RateBps, c.Amount, c.Status = e.level, e.rate, amount, CommissionAvailable
			c.CreatedAt, c.UpdatedAt = now, now
			receipt, err := s.deposit(ctx, actor, e.affiliate, "affiliate:commission:"+c.ID, amount,
				wallet.Meta{"commission": c.ID, "order": ref, "level": strconv.Itoa(e.level)})
			if err != nil {
				return err
			}
			c.WalletOperationID = receipt.Operation.ID
			if _, err := c.Save(ctx, g); err != nil {
				return err
			}
			paid = append(paid, c)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			// The same payment reported twice at once: the first report is
			// the one that stands.
			if winner, ferr := Conversions(s.db).Where("order_ref", "=", ref).First(ctx, g); ferr == nil && winner != nil {
				won, err := Commissions(s.db).Where("conversion_id", "=", winner.ID).OrderBy("level").Get(ctx, g)
				return winner, won, err
			}
		}
		return nil, nil, err
	}
	return conversion, paid, nil
}

// walletOf is the affiliate's wallet, opened the first time it is needed:
// its money leaves only through a withdrawal request this package asks for.
func (s *AffiliateService) walletOf(ctx context.Context, actor security.Subject, a *Affiliate) (*wallet.Wallet, error) {
	w, err := s.wallets.FindBySlug(ctx, actor, a.UserID, WalletSlug)
	if errors.Is(err, wallet.ErrNotFound) {
		w, err = s.wallets.Open(ctx, actor, wallet.OpenRequest{
			HolderID: a.UserID, Slug: WalletSlug, Name: "Afiliado", Description: "Comissões do programa de afiliados",
			Currency: "BRL", DecimalPlaces: 2, PayoutVia: PayoutVia,
		})
	}
	return w, err
}

// deposit puts centavos in the affiliate's wallet under a key that makes it
// happen once. The actor is the one who records the payment, and arandu-wallet
// asks its own policy whether they may move money.
func (s *AffiliateService) deposit(ctx context.Context, actor security.Subject, a *Affiliate, key string, amount int64, meta wallet.Meta) (wallet.Receipt, error) {
	w, err := s.walletOf(ctx, actor, a)
	if err != nil {
		return wallet.Receipt{}, err
	}
	return s.wallets.Deposit(ctx, actor, wallet.DepositRequest{IdempotencyKey: key, WalletID: w.ID, Amount: cents(amount), Meta: meta})
}

// RecordRefund records that a payment was given back, and reverses every
// commission it paid. The money leaves each wallet whatever its balance: a
// commission already withdrawn leaves the balance below zero, which blocks
// withdrawals until later commissions, or the admin's write-off, cover it.
// Refunding twice changes nothing.
func (s *AffiliateService) RecordRefund(ctx context.Context, actor security.Subject, orderRef, reason string) (*Conversion, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	conversion, err := Conversions(s.db).Where("order_ref", "=", strings.TrimSpace(orderRef)).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if conversion == nil {
		return nil, ErrNotFound
	}
	if conversion.Status == ConversionRefunded {
		return conversion, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "reembolso do pedido " + conversion.OrderRef
	}
	paid, err := Commissions(s.db).Where("conversion_id", "=", conversion.ID).Where("status", "=", string(CommissionAvailable)).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		for _, c := range paid {
			if err := s.reverse(ctx, actor, g, c, reason); err != nil {
				return err
			}
		}
		conversion.Status, conversion.UpdatedAt = ConversionRefunded, s.now()
		_, err := conversion.Save(ctx, g)
		return err
	})
	if err != nil {
		return nil, err
	}
	return conversion, nil
}

// ReverseCommission takes one commission back, by the admin's hand, with the
// reason the affiliate reads.
func (s *AffiliateService) ReverseCommission(ctx context.Context, actor security.Subject, commissionID, reason string) (*Commission, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, validation.Errors{"reason": {"is required"}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	c, err := Commissions(s.db).WhereKey(commissionID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrNotFound
	}
	if c.Status == CommissionReversed {
		return c, nil
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		return s.reverse(ctx, actor, g, c, reason)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// reverse takes a commission out of its wallet, forced past the balance, and
// marks it reversed, inside the caller's transaction.
func (s *AffiliateService) reverse(ctx context.Context, actor security.Subject, g security.Grant, c *Commission, reason string) error {
	owner, err := Affiliates(s.db).WhereKey(c.AffiliateID).First(ctx, g)
	if err != nil {
		return err
	}
	if owner == nil {
		return ErrNotFound
	}
	w, err := s.walletOf(ctx, actor, owner)
	if err != nil {
		return err
	}
	receipt, err := s.wallets.Withdraw(ctx, actor, wallet.WithdrawRequest{
		IdempotencyKey: "affiliate:reversal:" + c.ID, WalletID: w.ID, Amount: cents(c.Amount), Force: true,
		Meta: wallet.Meta{"commission": c.ID, "reason": truncate(reason, 200)},
	})
	if err != nil {
		return err
	}
	c.Status, c.ReversalOperationID, c.ReversedReason = CommissionReversed, receipt.Operation.ID, truncate(reason, maxTextLen)
	c.UpdatedAt = s.now()
	_, err = c.Save(ctx, g)
	return err
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > n {
		return string([]rune(s)[:n])
	}
	return s
}

// WriteOff forgives an affiliate's negative balance: a deposit of what is
// missing, recorded in the ledger with the admin's reason. It answers the
// centavos written off, zero when the balance was not negative.
func (s *AffiliateService) WriteOff(ctx context.Context, actor security.Subject, affiliateID, reason string) (int64, error) {
	if strings.TrimSpace(reason) == "" {
		return 0, validation.Errors{"reason": {"is required"}}
	}
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return 0, err
	}
	owner, err := Affiliates(s.db).WhereKey(affiliateID).First(ctx, g)
	if err != nil {
		return 0, err
	}
	if owner == nil {
		return 0, ErrNotFound
	}
	w, err := s.walletOf(ctx, actor, owner)
	if err != nil {
		return 0, err
	}
	missing := -int64(w.Balance)
	if missing <= 0 {
		return 0, nil
	}
	key := fmt.Sprintf("affiliate:write-off:%s:%d", owner.ID, w.LastSequence)
	if _, err := s.wallets.Deposit(ctx, actor, wallet.DepositRequest{
		IdempotencyKey: key, WalletID: w.ID, Amount: cents(missing),
		Meta: wallet.Meta{"kind": "write_off", "reason": truncate(reason, 200)},
	}); err != nil {
		return 0, err
	}
	return missing, nil
}

// CommissionsQuery narrows a list of commissions. Page counts from one.
type CommissionsQuery struct {
	AffiliateID string
	Level       int
	Status      CommissionStatus
	From, To    Day
	Sort        string
	Dir         string
	Page        int
	PerPage     int
}

// sortableCommission is the ordering allowlist.
var sortableCommission = map[string]string{
	"": "created_at", "created_at": "created_at", "amount": "amount", "level": "level", "status": "status",
}

// CommissionView is a commission with the payment it was paid on and the
// affiliate who earned it.
type CommissionView struct {
	Commission Commission
	Conversion *Conversion
	Affiliate  *Affiliate
}

// CommissionsPage is a page of commissions and how many the list has: an
// affiliate's own, or, for the admin, anybody's.
func (s *AffiliateService) CommissionsPage(ctx context.Context, actor security.Subject, q CommissionsQuery) ([]CommissionView, int, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, 0, err
	}
	if !actor.HasRole(AdminRole) {
		me, err := Affiliates(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
		if err != nil {
			return nil, 0, err
		}
		if me == nil {
			return nil, 0, forbidden("affiliate: %s is no affiliate of this tenant", actor.ID)
		}
		if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *me); err != nil {
			return nil, 0, err
		}
		q.AffiliateID = me.ID
	}
	column, ok := sortableCommission[q.Sort]
	if !ok {
		return nil, 0, validation.Errors{"sort": {"is not a column the list can be ordered by"}}
	}
	if q.PerPage <= 0 || q.PerPage > maxLimit {
		q.PerPage = defaultLimit
	}
	if q.Page < 1 {
		q.Page = 1
	}
	loc := s.location()
	filtered := func() *CommissionQuery {
		b := Commissions(s.db)
		if q.AffiliateID != "" {
			b = b.Where("affiliate_id", "=", q.AffiliateID)
		}
		if q.Level > 0 {
			b = b.Where("level", "=", q.Level)
		}
		if q.Status != "" {
			b = b.Where("status", "=", string(q.Status))
		}
		if !q.From.IsZero() {
			b = b.Where("created_at", ">=", dayStart(q.From, loc))
		}
		if !q.To.IsZero() {
			b = b.Where("created_at", "<", dayStart(q.To, loc).AddDate(0, 0, 1))
		}
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	page := filtered()
	if q.Dir == "asc" {
		page = page.OrderBy(column)
	} else {
		page = page.OrderByDesc(column)
	}
	rows, err := page.OrderByDesc("id").Offset((q.Page-1)*q.PerPage).Limit(q.PerPage).Get(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	conversionIDs, affiliateIDs := make([]any, 0, len(rows)), make([]any, 0, len(rows))
	for _, c := range rows {
		conversionIDs = append(conversionIDs, c.ConversionID)
		affiliateIDs = append(affiliateIDs, c.AffiliateID)
	}
	conversions, affiliates := map[string]*Conversion{}, map[string]*Affiliate{}
	if len(rows) > 0 {
		found, err := Conversions(s.db).WhereIn("id", conversionIDs).Get(ctx, g)
		if err != nil {
			return nil, 0, err
		}
		for _, c := range found {
			conversions[c.ID] = c
		}
		earners, err := Affiliates(s.db).WhereIn("id", affiliateIDs).Get(ctx, g)
		if err != nil {
			return nil, 0, err
		}
		for _, a := range earners {
			affiliates[a.ID] = a
		}
	}
	out := make([]CommissionView, 0, len(rows))
	for _, c := range rows {
		out = append(out, CommissionView{Commission: *c, Conversion: conversions[c.ConversionID], Affiliate: affiliates[c.AffiliateID]})
	}
	return out, int(total), nil
}

// dayStart is midnight of a day in a timezone, in UTC.
func dayStart(d Day, loc *time.Location) time.Time {
	t := d.Time()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).UTC()
}

// Stats are an affiliate's numbers over a period.
type Stats struct {
	Clicks, Signups, Sales int64
	Commissions            int64
}

// StatsOf counts an affiliate's clicks, sign-ups and sales in [from, to],
// and sums the commissions they earned in it.
func (s *AffiliateService) StatsOf(ctx context.Context, actor security.Subject, affiliateID string, from, to Day) (Stats, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return Stats{}, err
	}
	owner, err := Affiliates(s.db).WhereKey(affiliateID).First(ctx, g)
	if err != nil {
		return Stats{}, err
	}
	if owner == nil {
		return Stats{}, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *owner); err != nil {
		return Stats{}, err
	}
	loc := s.location()
	start, end := dayStart(from, loc), dayStart(to, loc).AddDate(0, 0, 1)
	var st Stats
	if st.Clicks, err = Clicks(s.db).Where("affiliate_id", "=", owner.ID).Where("at", ">=", start).Where("at", "<", end).Count(ctx, g); err != nil {
		return Stats{}, err
	}
	if st.Signups, err = Referrals(s.db).Where("affiliate_id", "=", owner.ID).Where("created_at", ">=", start).Where("created_at", "<", end).Count(ctx, g); err != nil {
		return Stats{}, err
	}
	if st.Sales, err = Conversions(s.db).Where("affiliate_id", "=", owner.ID).Where("status", "=", string(ConversionCounted)).
		Where("occurred_at", ">=", start).Where("occurred_at", "<", end).Count(ctx, g); err != nil {
		return Stats{}, err
	}
	earned, err := Commissions(s.db).Where("affiliate_id", "=", owner.ID).Where("status", "=", string(CommissionAvailable)).
		Where("created_at", ">=", start).Where("created_at", "<", end).Get(ctx, g)
	if err != nil {
		return Stats{}, err
	}
	for _, c := range earned {
		st.Commissions += c.Amount
	}
	return st, nil
}
