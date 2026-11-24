package feature_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

// The commercial rules of a plan: how long a buyer pays commission, the share
// of an annual payment, the grace period, the buyer who already paid, and the
// coupon. Every rule is off at zero, and the last test holds that.

const (
	monthly = 2490  // R$ 24,90: the monthly price the rules are checked against
	annual  = 23880 // R$ 238,80: the annual price
)

// at is a payment instant, noon UTC of a day of 2026 or 2027.
func at(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

// today is the instant a payment is made when its grace period has to still be
// running: the service reads the real clock, and a commission whose release date
// has passed is deposited at once.
func today() time.Time { return time.Now().UTC().Truncate(time.Second) }

// rulesPlan activates a linear plan of 30% with the rules given.
func rulesPlan(t *testing.T, w world, rules affiliate.PlanRequest) *affiliate.PlanView {
	t.Helper()
	rules.Name, rules.Kind, rules.Rates = "Contrato", affiliate.Linear, []int64{3000}
	rules.MinWithdrawal = 10000
	p, err := w.svc.CreatePlan(context.Background(), admin(), rules, true)
	if err != nil {
		t.Fatalf("defining the plan: %v", err)
	}
	return p
}

// payAs records a payment with every field the rules read.
func payAs(t *testing.T, w world, in affiliate.ConversionRequest) (*affiliate.Conversion, []*affiliate.Commission) {
	t.Helper()
	if in.ProductID == "" {
		in.ProductID = "corujao"
	}
	conversion, paid, err := w.svc.RecordConversion(context.Background(), admin(), in)
	if err != nil {
		t.Fatalf("recording %s: %v", in.OrderRef, err)
	}
	return conversion, paid
}

// sum is the centavos of a list of commissions.
func sum(paid []*affiliate.Commission) int64 {
	var total int64
	for _, c := range paid {
		total += c.Amount
	}
	return total
}

func TestACommissionLastsSixMonthsFromTheFirstPayment(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{MaxMonths: 6})
	signUp(t, w, "buyer-1", "ana", "")

	// Seven monthly payments of R$ 24,90, on the 10th of each month. 30% is
	// R$ 7,47 each; the sixth is the last inside six months, and the seventh,
	// exactly six months after the first, is recorded and pays nothing.
	var total int64
	for i := range 7 {
		conversion, paid := payAs(t, w, affiliate.ConversionRequest{
			OrderRef: "mensal-" + string(rune('1'+i)), BuyerUserID: "buyer-1", Amount: monthly, Renewal: i > 0,
			OccurredAt: at(2026, time.Month(1+i), 10),
		})
		if i < 6 {
			if len(paid) != 1 || paid[0].Amount != 747 || conversion.SkippedReason != affiliate.SkipNone {
				t.Fatalf("payment %d paid %+v, skipped %q; want one commission of 747", i+1, paid, conversion.SkippedReason)
			}
		} else if len(paid) != 0 || conversion.SkippedReason != affiliate.SkipDurationLimit {
			t.Fatalf("the seventh payment paid %+v, skipped %q; want nothing, %q", paid, conversion.SkippedReason, affiliate.SkipDurationLimit)
		}
		total += sum(paid)
	}
	if total != 4482 || balance(t, w, ana) != 4482 {
		t.Fatalf("six months paid %d and the wallet holds %d; want R$ 44,82", total, balance(t, w, ana))
	}

	// The seventh payment is on the record, and reporting it again changes nothing.
	again, paid := payAs(t, w, affiliate.ConversionRequest{
		OrderRef: "mensal-7", BuyerUserID: "buyer-1", Amount: monthly, Renewal: true, OccurredAt: at(2026, 7, 10),
	})
	if again.SkippedReason != affiliate.SkipDurationLimit || len(paid) != 0 || again.Status != affiliate.ConversionCounted {
		t.Fatalf("the repeated report answered %+v with %d commissions", again, len(paid))
	}
}

func TestTheDurationIsCountedInCalendarMonthsClampedToTheMonthEnd(t *testing.T) {
	t.Parallel()
	w := install(t)
	enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{MaxMonths: 6})
	signUp(t, w, "buyer-1", "ana", "")

	// The first payment is on August 31: six months later is February 28, 2027.
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: at(2026, 8, 31)})
	if c, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-2", BuyerUserID: "buyer-1", Amount: monthly, Renewal: true, OccurredAt: at(2027, 2, 27)}); len(paid) != 1 {
		t.Fatalf("the payment of February 27 paid %d commissions, skipped %q", len(paid), c.SkippedReason)
	}
	if c, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-3", BuyerUserID: "buyer-1", Amount: monthly, Renewal: true, OccurredAt: at(2027, 2, 28)}); len(paid) != 0 || c.SkippedReason != affiliate.SkipDurationLimit {
		t.Fatalf("the payment of February 28 paid %d commissions, skipped %q; want the limit", len(paid), c.SkippedReason)
	}
}

func TestACommissionCanEndAfterANumberOfPayments(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{MaxPayments: 3})
	signUp(t, w, "buyer-1", "ana", "")

	for i, ref := range []string{"o-1", "o-2", "o-3", "o-4", "o-5"} {
		conversion, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: ref, BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: at(2026, time.Month(1+i), 10)})
		if want := i < 3; (len(paid) == 1) != want {
			t.Fatalf("payment %d paid %d commissions, skipped %q; want paid=%v", i+1, len(paid), conversion.SkippedReason, want)
		}
		if i >= 3 && conversion.SkippedReason != affiliate.SkipPaymentLimit {
			t.Fatalf("payment %d skipped %q, want %q", i+1, conversion.SkippedReason, affiliate.SkipPaymentLimit)
		}
	}
	if got := balance(t, w, ana); got != 3*747 {
		t.Fatalf("ana holds %d, want three commissions of 747", got)
	}
}

func TestAnAnnualPaymentPaysOnItsShareOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{MaxMonths: 6, AnnualBaseBps: 5000})
	signUp(t, w, "buyer-1", "ana", "")

	// R$ 238,80 annual: 30% of half of it is R$ 35,82.
	conversion, paid := payAs(t, w, affiliate.ConversionRequest{
		OrderRef: "anual-1", BuyerUserID: "buyer-1", Amount: annual, Annual: true, OccurredAt: at(2026, 3, 5),
	})
	if len(paid) != 1 || paid[0].Amount != 3582 || paid[0].BaseAmount != 11940 || !bool(conversion.Annual) {
		t.Fatalf("the annual payment paid %+v; want 3582 on a base of 11940", paid)
	}
	if got := balance(t, w, ana); got != 3582 {
		t.Fatalf("ana holds %d, want R$ 35,82", got)
	}

	// The renewal a year later is past the six months: recorded, paying nothing.
	renewal, paid := payAs(t, w, affiliate.ConversionRequest{
		OrderRef: "anual-2", BuyerUserID: "buyer-1", Amount: annual, Annual: true, Renewal: true, OccurredAt: at(2027, 3, 5),
	})
	if len(paid) != 0 || renewal.SkippedReason != affiliate.SkipDurationLimit {
		t.Fatalf("the annual renewal paid %+v, skipped %q; want the limit", paid, renewal.SkippedReason)
	}
	if got := balance(t, w, ana); got != 3582 {
		t.Fatalf("ana holds %d after the renewal, want it unchanged", got)
	}

	// A monthly payment is not cut by the annual share.
	signUp(t, w, "buyer-2", "ana", "")
	if _, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "mensal-1", BuyerUserID: "buyer-2", Amount: monthly, OccurredAt: at(2026, 3, 5)}); len(paid) != 1 || paid[0].Amount != 747 {
		t.Fatalf("a monthly payment paid %+v, want 747", paid)
	}
}

func TestACommissionWaitsOutTheGracePeriodAndIsReleasedOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{GraceDays: 30})
	signUp(t, w, "buyer-1", "ana", "")
	ctx := context.Background()

	paidAt := today()
	_, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: paidAt})
	if len(paid) != 1 || paid[0].Status != affiliate.CommissionPending || paid[0].ReleaseAt == nil || !paid[0].ReleaseAt.Equal(paidAt.AddDate(0, 0, 30)) {
		t.Fatalf("the commission is %+v; want pending until 30 days after the payment", paid)
	}
	b, err := w.svc.BalanceOf(ctx, admin(), ana.ID)
	if err != nil || b.Balance != 0 || b.Available != 0 || b.Pending != 747 {
		t.Fatalf("balance during the grace period is %+v, %v; want nothing in the wallet and 747 pending", b, err)
	}
	stats, _ := w.svc.StatsOf(ctx, admin(), ana.ID, "2026-01-01", "2100-01-01")
	if stats.Pending != 747 || stats.Commissions != 0 {
		t.Fatalf("the period shows %+v; want 747 pending and none released", stats)
	}

	// The day before the grace period ends nothing moves.
	if n, err := w.svc.ReleaseDue(ctx, admin(), paidAt.AddDate(0, 0, 29)); err != nil || n != 0 {
		t.Fatalf("releasing a day early released %d, %v", n, err)
	}
	if got := balance(t, w, ana); got != 0 {
		t.Fatalf("the wallet holds %d before the release", got)
	}

	// On the day it does, once, however many times the job runs.
	due := paidAt.AddDate(0, 0, 30)
	if n, err := w.svc.ReleaseDue(ctx, admin(), due); err != nil || n != 1 {
		t.Fatalf("releasing on the day released %d, %v; want 1", n, err)
	}
	if n, err := w.svc.ReleaseDue(ctx, admin(), due.AddDate(0, 0, 5)); err != nil || n != 0 {
		t.Fatalf("a second run released %d, %v; want 0", n, err)
	}
	if got := balance(t, w, ana); got != 747 {
		t.Fatalf("the wallet holds %d after the release, want 747", got)
	}
	b, _ = w.svc.BalanceOf(ctx, admin(), ana.ID)
	if b.Pending != 0 || b.Available != 747 {
		t.Fatalf("balance after the release is %+v", b)
	}
	rows, _, err := w.svc.CommissionsPage(ctx, admin(), affiliate.CommissionsQuery{AffiliateID: ana.ID})
	if err != nil || len(rows) != 1 || rows[0].Commission.Status != affiliate.CommissionAvailable || rows[0].Commission.WalletOperationID == "" {
		t.Fatalf("the commission after the release is %+v, %v", rows, err)
	}
}

func TestOnlyTheProgramsAdminReleasesCommissions(t *testing.T) {
	t.Parallel()
	w := install(t)
	if _, err := w.svc.ReleaseDue(context.Background(), user("buyer-1"), at(2026, 1, 1)); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a user released commissions: %v", err)
	}
	if _, err := w.svc.ReleaseDue(context.Background(), security.Guest(tenant), at(2026, 1, 1)); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a guest released commissions: %v", err)
	}
}

func TestARefundInsideTheGraceCancelsTheCommissionWithoutTouchingTheWallet(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{GraceDays: 30})
	signUp(t, w, "buyer-1", "ana", "")
	ctx := context.Background()

	paidAt := today()
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: paidAt})
	if _, err := w.svc.RecordRefund(ctx, admin(), "o-1", "chargeback"); err != nil {
		t.Fatalf("refunding: %v", err)
	}
	rows, _, _ := w.svc.CommissionsPage(ctx, admin(), affiliate.CommissionsQuery{AffiliateID: ana.ID})
	if len(rows) != 1 || rows[0].Commission.Status != affiliate.CommissionCancelled || rows[0].Commission.ReversalOperationID != "" || rows[0].Commission.ReversedReason != "chargeback" {
		t.Fatalf("the commission after the refund is %+v", rows)
	}
	b, _ := w.svc.BalanceOf(ctx, admin(), ana.ID)
	if b.Balance != 0 || b.Pending != 0 {
		t.Fatalf("balance after the refund is %+v; want nothing anywhere", b)
	}
	// The grace period ending later releases nothing, and the wallet was never opened.
	if n, err := w.svc.ReleaseDue(ctx, admin(), paidAt.AddDate(0, 0, 60)); err != nil || n != 0 {
		t.Fatalf("releasing a cancelled commission released %d, %v", n, err)
	}
	if got := balance(t, w, ana); got != 0 {
		t.Fatalf("the wallet holds %d", got)
	}
	// Refunding again changes nothing.
	if _, err := w.svc.RecordRefund(ctx, admin(), "o-1", ""); err != nil {
		t.Fatalf("refunding twice: %v", err)
	}
}

func TestARefundAfterTheReleaseTakesTheMoneyBackOutOfTheWallet(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{GraceDays: 30})
	signUp(t, w, "buyer-1", "ana", "")
	ctx := context.Background()

	paidAt := today()
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: paidAt})
	if _, err := w.svc.ReleaseDue(ctx, admin(), paidAt.AddDate(0, 0, 31)); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	// A second commission, still pending, is cancelled with the same refund of
	// its own payment while the first is reversed out of the wallet.
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-2", BuyerUserID: "buyer-1", Amount: monthly, Renewal: true, OccurredAt: paidAt.AddDate(0, 0, 30)})
	if _, err := w.svc.RecordRefund(ctx, admin(), "o-1", ""); err != nil {
		t.Fatalf("refunding: %v", err)
	}
	if got := balance(t, w, ana); got != 0 {
		t.Fatalf("the wallet holds %d after refunding the released commission, want 0", got)
	}
	rows, _, _ := w.svc.CommissionsPage(ctx, admin(), affiliate.CommissionsQuery{AffiliateID: ana.ID, Status: affiliate.CommissionReversed})
	if len(rows) != 1 || rows[0].Commission.ReversalOperationID == "" {
		t.Fatalf("the reversed commissions are %+v", rows)
	}
	b, _ := w.svc.BalanceOf(ctx, admin(), ana.ID)
	if b.Pending != 747 {
		t.Fatalf("the second payment's commission is %+v; want it still pending", b)
	}
}

func TestACommissionOfAPaymentAlreadyPastItsGraceIsDepositedAtOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{GraceDays: 30})
	signUp(t, w, "buyer-1", "ana", "")

	// A payment reported long after it happened has no grace left to wait out.
	_, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: at(2025, 1, 10)})
	if len(paid) != 1 || paid[0].Status != affiliate.CommissionAvailable || balance(t, w, ana) != 747 {
		t.Fatalf("the late report paid %+v, the wallet holds %d", paid, balance(t, w, ana))
	}
}

func TestABuyerWhoAlreadyPaidEarnsTheAffiliateNothing(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{PriorPayerMonths: 12})

	// Ana's link reaches somebody who paid R$ 24,90 eight months before: the
	// payment is recorded, there is no commission, and none follows.
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "old-1", BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: at(2026, 1, 10)})
	signUp(t, w, "buyer-1", "ana", "")
	first, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, Renewal: true, OccurredAt: at(2026, 9, 10)})
	if len(paid) != 0 || first.SkippedReason != affiliate.SkipPriorPayer || first.AffiliateID == "" {
		t.Fatalf("the prior payer's payment paid %+v, skipped %q", paid, first.SkippedReason)
	}
	if next, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-2", BuyerUserID: "buyer-1", Amount: monthly, Renewal: true, OccurredAt: at(2026, 10, 10)}); len(paid) != 0 || next.SkippedReason != affiliate.SkipPriorPayer {
		t.Fatalf("the next payment of a prior payer paid %+v, skipped %q", paid, next.SkippedReason)
	}

	// A payment older than the look-back does not count.
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "old-2", BuyerUserID: "buyer-2", Amount: monthly, OccurredAt: at(2025, 1, 10)})
	signUp(t, w, "buyer-2", "ana", "")
	if _, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-3", BuyerUserID: "buyer-2", Amount: monthly, Renewal: true, OccurredAt: at(2026, 9, 10)}); len(paid) != 1 {
		t.Fatalf("a payment 20 months earlier blocked the commission: %d paid", len(paid))
	}
	// A buyer who never paid before earns it.
	signUp(t, w, "buyer-3", "ana", "")
	if _, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-4", BuyerUserID: "buyer-3", Amount: monthly, OccurredAt: at(2026, 9, 10)}); len(paid) != 1 {
		t.Fatalf("a new buyer paid %d commissions, want 1", len(paid))
	}
	if got := balance(t, w, ana); got != 2*747 {
		t.Fatalf("ana holds %d, want two commissions", got)
	}
}

func TestAPriorPaymentUnderAnotherLoginOfTheSameDocumentIsFoundByTheModule(t *testing.T) {
	t.Parallel()
	w := install(t)
	enroll(t, w, "user-ana", "ana", cpfAna)
	rulesPlan(t, w, affiliate.PlanRequest{PriorPayerMonths: 12})

	payAs(t, w, affiliate.ConversionRequest{OrderRef: "old-1", BuyerUserID: "login-1", BuyerDocument: "390.533.447-05", Amount: monthly, OccurredAt: at(2026, 3, 10)})
	signUp(t, w, "login-2", "ana", cpfCaio)
	conversion, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "login-2", BuyerDocument: cpfCaio, Amount: monthly, OccurredAt: at(2026, 8, 10)})
	if len(paid) != 0 || conversion.SkippedReason != affiliate.SkipPriorPayer || conversion.BuyerDocument != cpfCaio {
		t.Fatalf("the second login of a payer paid %+v, skipped %q, document %q", paid, conversion.SkippedReason, conversion.BuyerDocument)
	}
	if _, _, err := w.svc.RecordConversion(context.Background(), admin(), affiliate.ConversionRequest{
		OrderRef: "bad", BuyerUserID: "login-3", BuyerDocument: "123", Amount: monthly, OccurredAt: at(2026, 8, 10),
	}); err == nil {
		t.Fatal("a document that is neither a CPF nor a CNPJ was accepted")
	}
}

func TestThePriorPayerFlagOfTheApplicationIsReadOnlyWhileThePlanAsksForIt(t *testing.T) {
	t.Parallel()
	w := install(t)
	enroll(t, w, "user-ana", "ana", cpfAna)
	signUp(t, w, "buyer-1", "ana", "")
	signUp(t, w, "buyer-2", "ana", "")

	// With the rule off, the flag changes nothing.
	rulesPlan(t, w, affiliate.PlanRequest{})
	if _, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-1", BuyerUserID: "buyer-1", Amount: monthly, PriorPayer: true, OccurredAt: at(2026, 9, 10)}); len(paid) != 1 {
		t.Fatalf("the flag blocked a commission under a plan that does not ask: %d paid", len(paid))
	}
	// With it on, the flag is the application's word for what the module cannot see.
	rulesPlan(t, w, affiliate.PlanRequest{PriorPayerMonths: 12})
	conversion, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-2", BuyerUserID: "buyer-2", Amount: monthly, PriorPayer: true, OccurredAt: at(2026, 9, 10)})
	if len(paid) != 0 || conversion.SkippedReason != affiliate.SkipPriorPayer {
		t.Fatalf("the flag paid %+v, skipped %q", paid, conversion.SkippedReason)
	}
}

func TestAPlanOfZeroValuesPaysAsItAlwaysDid(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	plan := planOf(t, w, affiliate.Linear, 10000, 0, 3000)
	if p := plan.Plan; p.MaxMonths != 0 || p.MaxPayments != 0 || p.GraceDays != 0 || p.PriorPayerMonths != 0 || p.AnnualBase() != affiliate.FullBaseBps {
		t.Fatalf("a plan that set no rule carries %+v", p)
	}
	signUp(t, w, "buyer-1", "ana", "")

	var total int64
	for i := range 30 {
		order := "o-" + time.Date(2026, time.Month(1+i%12), 1+i/12, 0, 0, 0, 0, time.UTC).Format("20060102")
		_, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: order, BuyerUserID: "buyer-1", Amount: monthly, OccurredAt: at(2026, 1, 10).AddDate(0, i, 0)})
		if len(paid) != 1 || paid[0].Status != affiliate.CommissionAvailable || paid[0].ReleaseAt != nil || paid[0].Amount != 747 {
			t.Fatalf("payment %d paid %+v; want 747 in the wallet at once", i+1, paid)
		}
		total += sum(paid)
	}
	// An annual payment pays the whole of it under such a plan.
	_, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "anual", BuyerUserID: "buyer-1", Amount: annual, Annual: true, OccurredAt: at(2029, 1, 10)})
	if len(paid) != 1 || paid[0].Amount != 7164 {
		t.Fatalf("the annual payment paid %+v; want 30%% of the whole, 7164", paid)
	}
	if got := balance(t, w, ana); got != total+7164 {
		t.Fatalf("ana holds %d, want %d", got, total+7164)
	}
	if n, err := w.svc.ReleaseDue(context.Background(), admin(), at(2030, 1, 1)); err != nil || n != 0 {
		t.Fatalf("a plan with no grace left %d to release, %v", n, err)
	}
}

func TestThePlanValidatesItsRules(t *testing.T) {
	t.Parallel()
	for name, req := range map[string]affiliate.PlanRequest{
		"negative months":    {MaxMonths: -1},
		"too many months":    {MaxMonths: affiliate.MaxRuleMonths + 1},
		"negative payments":  {MaxPayments: -1},
		"annual above 100%":  {AnnualBaseBps: 10001},
		"negative annual":    {AnnualBaseBps: -5},
		"negative grace":     {GraceDays: -1},
		"too long a grace":   {GraceDays: affiliate.MaxGraceDays + 1},
		"negative look-back": {PriorPayerMonths: -1},
	} {
		req.Name, req.Kind, req.Rates = "Plano", affiliate.Linear, []int64{1000}
		if !req.Validate().Any() {
			t.Errorf("%s was accepted", name)
		}
	}
	ok := affiliate.PlanRequest{Name: "Plano", Kind: affiliate.Linear, Rates: []int64{3000}, MaxMonths: 6, MaxPayments: 6, AnnualBaseBps: 5000, GraceDays: 30, PriorPayerMonths: 12}
	if errs := ok.Validate(); errs.Any() {
		t.Fatalf("the contract's plan was refused: %v", errs)
	}
}

// sameErrors reports whether err is the validation error the package exports,
// which is a map and so is compared by what it says.
func sameErrors(err error, want error) bool {
	if err == nil {
		return false
	}
	return err.Error() == want.Error() || errors.Is(err, want)
}
