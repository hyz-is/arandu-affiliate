package affiliate

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/filesystem"
	wallet "github.com/hyz-is/arandu-wallet"
)

// The refusals of a withdrawal.
var (
	// ErrOnePerDay is a second withdrawal on a day that already has one.
	ErrOnePerDay = validation.Errors{"amount": {"one withdrawal a day; ask again tomorrow"}}
	// ErrNegativeBalance is a withdrawal while a reversed commission left the
	// balance below zero.
	ErrNegativeBalance = validation.Errors{"amount": {"the balance is below zero after a reversed commission; withdrawals open again once it is covered"}}
	// ErrBlocked is a blocked affiliate asking for money.
	ErrBlocked = forbidden("affiliate: a blocked affiliate cannot withdraw")
)

// Balance is an affiliate's money as the panel shows it, in centavos.
type Balance struct {
	// Balance is what the wallet holds, negative after a reversal of money
	// already withdrawn.
	Balance int64
	// Held is what open withdrawal requests set aside.
	Held int64
	// Available is what can be asked for now.
	Available int64
}

// balanceOf reads the affiliate's wallet; an affiliate who never earned has
// none, and a zero balance.
func (s *AffiliateService) balanceOf(ctx context.Context, actor security.Subject, a *Affiliate) (Balance, *wallet.Wallet, error) {
	w, err := s.wallets.FindBySlug(ctx, actor, a.UserID, WalletSlug)
	if errors.Is(err, wallet.ErrNotFound) {
		return Balance{}, nil, nil
	}
	if err != nil {
		return Balance{}, nil, err
	}
	b := Balance{Balance: int64(w.Balance), Held: int64(w.Held)}
	if b.Available = b.Balance - b.Held; b.Available < 0 {
		b.Available = 0
	}
	return b, w, nil
}

// BalanceOf is an affiliate's balance, for themselves and for the admin.
func (s *AffiliateService) BalanceOf(ctx context.Context, actor security.Subject, affiliateID string) (Balance, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return Balance{}, err
	}
	owner, err := Affiliates(s.db).WhereKey(affiliateID).First(ctx, g)
	if err != nil {
		return Balance{}, err
	}
	if owner == nil {
		return Balance{}, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *owner); err != nil {
		return Balance{}, err
	}
	b, _, err := s.balanceOf(ctx, actor, owner)
	return b, err
}

// PayoutKinds are the kinds of account an affiliate can be paid into, and
// the details each one takes.
var PayoutKinds = map[string][]string{
	"pix":    {"key_type", "key"},
	"bank":   {"bank", "agency", "account", "account_type"},
	"crypto": {"network", "address"},
	"other":  {"description"},
}

// PayoutKindNames are PayoutKinds' keys in the order a form lists them.
var PayoutKindNames = []string{"pix", "bank", "crypto", "other"}

// AccountRequest is a payout account an affiliate declares.
type AccountRequest struct {
	Kind    string
	Label   string
	Details map[string]string
}

// AddPayoutAccount declares an account the asking affiliate is paid into, in
// the name and the document of their registration: an affiliate is paid into
// an account of their own, never a third party's.
func (s *AffiliateService) AddPayoutAccount(ctx context.Context, actor security.Subject, in AccountRequest) (*wallet.PayoutAccount, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, err
	}
	me, err := Affiliates(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if me == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *me); err != nil {
		return nil, err
	}
	fields, ok := PayoutKinds[in.Kind]
	if !ok {
		return nil, validation.Errors{"kind": {"must be one of " + strings.Join(PayoutKindNames, ", ")}}
	}
	details := wallet.Meta{}
	errs := validation.Errors{}
	for _, f := range fields {
		v := strings.TrimSpace(in.Details[f])
		validation.Required(errs, f, v)
		validation.MaxLen(errs, f, v, maxNameLen)
		details[f] = v
	}
	if errs.Any() {
		return nil, errs
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = strings.ToUpper(in.Kind)
		if in.Kind == "pix" {
			label = "PIX " + details["key"]
		}
	}
	return s.wallets.AddPayoutAccount(ctx, actor, wallet.PayoutAccountRequest{
		HolderID: me.UserID, Kind: in.Kind, Label: truncate(label, maxNameLen), Details: details,
		HolderName: me.Name, HolderDocument: me.Document,
	})
}

// PayoutAccounts are an affiliate's accounts, for themselves and the admin.
func (s *AffiliateService) PayoutAccounts(ctx context.Context, actor security.Subject, affiliateID string) ([]*wallet.PayoutAccount, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, err
	}
	owner, err := Affiliates(s.db).WhereKey(affiliateID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *owner); err != nil {
		return nil, err
	}
	return s.wallets.PayoutAccountsOf(ctx, actor, owner.UserID)
}

// WithdrawalRequest is what an affiliate asks for.
type WithdrawalRequest struct {
	// Amount is centavos; zero asks for everything available.
	Amount    int64
	AccountID string
}

// RequestWithdrawal asks for an affiliate's money: any amount between the
// plan's minimum and maximum, up to what is available, at most once a day,
// never while the balance is below zero. arandu-wallet holds the amount until
// the operator pays it or refuses it; a refused or cancelled request frees
// the day.
func (s *AffiliateService) RequestWithdrawal(ctx context.Context, actor security.Subject, in WithdrawalRequest) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, err
	}
	me, err := Affiliates(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if me == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *me); err != nil {
		return nil, err
	}
	if !me.IsActive() {
		return nil, ErrBlocked
	}
	if strings.TrimSpace(in.AccountID) == "" {
		return nil, validation.Errors{"account_id": {"choose the account to be paid into"}}
	}
	balance, w, err := s.balanceOf(ctx, actor, me)
	if err != nil {
		return nil, err
	}
	if balance.Balance < 0 {
		return nil, ErrNegativeBalance
	}
	plan, err := s.activePlan(ctx, g)
	if err != nil {
		return nil, err
	}
	min, max, _ := s.limits(plan)
	amount := in.Amount
	if amount == 0 {
		amount = balance.Available
		if max > 0 && amount > max {
			amount = max
		}
	}
	switch {
	case w == nil || amount <= 0 || amount > balance.Available:
		return nil, validation.Errors{"amount": {"is more than the balance available"}}
	case amount < min:
		return nil, validation.Errors{"amount": {"is below the minimum of " + brl(min)}}
	case max > 0 && amount > max:
		return nil, validation.Errors{"amount": {"is above the maximum of " + brl(max)}}
	}

	day, err := s.claimDay(ctx, g, actor, me)
	if err != nil {
		return nil, err
	}
	req, err := s.wallets.RequestWithdrawal(ctx, actor, wallet.WithdrawalRequestInput{
		IdempotencyKey: "affiliate:withdrawal:" + me.ID + ":" + string(day.Day) + ":" + strconv.Itoa(day.Attempt),
		WalletID:       w.ID,
		Amount:         cents(amount),
		AccountID:      in.AccountID,
		Reference:      "affiliate:" + me.ID,
		Via:            PayoutVia,
	})
	if err != nil {
		// The wallet refused, so nothing was asked for: the day is free again.
		day.WithdrawalRequestID, day.UpdatedAt = dayFree, s.now()
		_, _ = day.Save(ctx, g)
		return nil, err
	}
	day.WithdrawalRequestID, day.UpdatedAt = req.ID, s.now()
	if _, err := day.Save(ctx, g); err != nil {
		return nil, err
	}
	return req, nil
}

// dayFree marks a day whose claim asked the wallet for nothing, because the
// wallet refused: the day may be taken again. An empty request is a claim
// still on its way to the wallet, and holds the day.
const dayFree = "-"

// claimDay takes today for the affiliate's withdrawal. The unique index on
// (tenant, affiliate, day) is what holds one a day under two requests at
// once. A day whose request was refused, cancelled, or never made is taken
// again, with the next attempt number.
func (s *AffiliateService) claimDay(ctx context.Context, g security.Grant, actor security.Subject, me *Affiliate) (*WithdrawalDay, error) {
	today := DayOf(s.now(), s.location())
	row, err := WithdrawalDays(s.db).New()
	if err != nil {
		return nil, err
	}
	if row.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	row.TenantID, row.AffiliateID, row.Day, row.Attempt = data.Tenant(g), me.ID, today, 1
	row.CreatedAt, row.UpdatedAt = now, now
	if _, err := row.Save(ctx, g); err == nil {
		return row, nil
	} else if !errors.Is(err, database.ErrUniqueViolation) {
		return nil, err
	}
	taken, err := WithdrawalDays(s.db).Where("affiliate_id", "=", me.ID).Where("day", "=", string(today)).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if taken == nil {
		return nil, ErrOnePerDay
	}
	switch taken.WithdrawalRequestID {
	case "":
		return nil, ErrOnePerDay
	case dayFree:
	default:
		mine, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{HolderID: me.UserID, Limit: 50})
		if err != nil {
			return nil, err
		}
		freed := false
		for _, r := range mine {
			if r.ID == taken.WithdrawalRequestID {
				freed = r.Status == wallet.WithdrawalRejected || r.Status == wallet.WithdrawalCancelled
			}
		}
		if !freed {
			return nil, ErrOnePerDay
		}
	}
	// The next attempt, guarded on the attempt read: two retries at once
	// cannot both take it.
	previous := taken.Attempt
	updated, err := WithdrawalDays(s.db).WhereKey(taken.ID).Where("attempt", "=", previous).
		Update(ctx, g, map[string]any{"attempt": previous + 1, "withdrawal_request_id": "", "updated_at": now})
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		return nil, ErrOnePerDay
	}
	taken.Attempt, taken.WithdrawalRequestID, taken.UpdatedAt = previous+1, "", now
	return taken, nil
}

// MyWithdrawals are the asking affiliate's withdrawal requests, newest first.
func (s *AffiliateService) MyWithdrawals(ctx context.Context, actor security.Subject) ([]*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, err
	}
	me, err := Affiliates(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if me == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *me); err != nil {
		return nil, err
	}
	_, w, err := s.balanceOf(ctx, actor, me)
	if err != nil || w == nil {
		return nil, err
	}
	return s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{WalletID: w.ID, Limit: 100})
}

// AffiliateWithdrawal is one affiliate's withdrawal request in the
// operator's queue.
type AffiliateWithdrawal struct {
	Request   *wallet.WithdrawalRequest
	Affiliate *Affiliate
}

// Withdrawals is the operator's queue of affiliates' withdrawals: the requests
// arandu-wallet holds that this package asked for.
func (s *AffiliateService) Withdrawals(ctx context.Context, actor security.Subject, status wallet.WithdrawalStatus) ([]AffiliateWithdrawal, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	days, err := WithdrawalDays(s.db).WhereNotIn("withdrawal_request_id", []any{"", dayFree}).Get(ctx, g)
	if err != nil {
		return nil, err
	}
	ours := map[string]string{}
	for _, d := range days {
		ours[d.WithdrawalRequestID] = d.AffiliateID
	}
	requests, err := s.wallets.WithdrawalRequestsOf(ctx, actor, wallet.WithdrawalFilter{Status: status, Limit: 200})
	if err != nil {
		return nil, err
	}
	owners := map[string]*Affiliate{}
	var out []AffiliateWithdrawal
	for _, r := range requests {
		affiliateID, ok := ours[r.ID]
		if !ok {
			continue
		}
		owner, seen := owners[affiliateID]
		if !seen {
			if owner, err = Affiliates(s.db).WhereKey(affiliateID).First(ctx, g); err != nil {
				return nil, err
			}
			owners[affiliateID] = owner
		}
		out = append(out, AffiliateWithdrawal{Request: r, Affiliate: owner})
	}
	return out, nil
}

// ours checks that a withdrawal request is one this package asked for, in
// the caller's tenant.
func (s *AffiliateService) ours(ctx context.Context, g security.Grant, requestID string) error {
	day, err := WithdrawalDays(s.db).Where("withdrawal_request_id", "=", requestID).First(ctx, g)
	if err != nil {
		return err
	}
	if day == nil {
		return ErrNotFound
	}
	return nil
}

// StartPaying is the operator taking an affiliate's withdrawal to pay it.
func (s *AffiliateService) StartPaying(ctx context.Context, actor security.Subject, requestID string) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	if _, err := WithdrawalDays(s.db).Where("withdrawal_request_id", "=", requestID).Count(ctx, g); err != nil {
		return nil, err
	}
	if err := s.ours(ctx, g, requestID); err != nil {
		return nil, err
	}
	return s.wallets.StartProcessing(ctx, actor, requestID)
}

// PayWithdrawal records that the operator paid an affiliate's withdrawal,
// with the end-to-end identifier and the receipt.
func (s *AffiliateService) PayWithdrawal(ctx context.Context, actor security.Subject, in wallet.PaymentInput, receipt io.Reader) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	if _, err := WithdrawalDays(s.db).Where("withdrawal_request_id", "=", in.RequestID).Count(ctx, g); err != nil {
		return nil, err
	}
	if err := s.ours(ctx, g, in.RequestID); err != nil {
		return nil, err
	}
	return s.wallets.PayWithdrawal(ctx, actor, in, receipt)
}

// RejectWithdrawal refuses an affiliate's withdrawal with the reason they
// read. The amount goes back to the balance, and the day is free again.
func (s *AffiliateService) RejectWithdrawal(ctx context.Context, actor security.Subject, requestID, reason string) (*wallet.WithdrawalRequest, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	if _, err := WithdrawalDays(s.db).Where("withdrawal_request_id", "=", requestID).Count(ctx, g); err != nil {
		return nil, err
	}
	if err := s.ours(ctx, g, requestID); err != nil {
		return nil, err
	}
	return s.wallets.RejectWithdrawal(ctx, actor, requestID, reason)
}

// WithdrawalReceipt opens the receipt of a paid withdrawal, for the affiliate
// it paid and for the admin. The caller closes the body.
func (s *AffiliateService) WithdrawalReceipt(ctx context.Context, actor security.Subject, requestID string) (*wallet.WithdrawalRequest, filesystem.File, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, filesystem.File{}, err
	}
	day, err := WithdrawalDays(s.db).Where("withdrawal_request_id", "=", requestID).First(ctx, g)
	if err != nil {
		return nil, filesystem.File{}, err
	}
	if day == nil {
		return nil, filesystem.File{}, ErrNotFound
	}
	owner, err := Affiliates(s.db).WhereKey(day.AffiliateID).First(ctx, g)
	if err != nil {
		return nil, filesystem.File{}, err
	}
	if owner == nil {
		return nil, filesystem.File{}, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *owner); err != nil {
		return nil, filesystem.File{}, err
	}
	return s.wallets.WithdrawalReceipt(ctx, actor, requestID)
}
