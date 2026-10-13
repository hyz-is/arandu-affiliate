package feature_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	wallet "github.com/hyz-is/arandu-wallet"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

// open is a visitor opening a URL with the cookies they carry.
func open(t *testing.T, router *fhttp.Router, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func cookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == affiliate.DefaultCookieName {
			return c
		}
	}
	return nil
}

func TestTheLinkSetsTheCookieAndTheLatestLinkReplacesIt(t *testing.T) {
	t.Parallel()
	w := install(t)
	enroll(t, w, "user-ana", "ana", cpfAna)
	enroll(t, w, "user-bruno", "bruno", cpfBruno)
	planOf(t, w, affiliate.Linear, 10000, 0, 150)

	router := fhttp.NewRouter()
	w.module.Routes(router.ForModule(w.module.Name()))

	first := open(t, router, "/r/ana?to=/planos")
	if first.Code != http.StatusSeeOther && first.Code != http.StatusFound {
		t.Fatalf("the link answered %d, want a redirect", first.Code)
	}
	if loc := first.Header().Get("Location"); loc != "/planos" {
		t.Fatalf("the link landed on %q, want /planos", loc)
	}
	c := cookieOf(first)
	if c == nil || c.Value != "ana" || c.MaxAge != 30*24*60*60 || !c.HttpOnly {
		t.Fatalf("the cookie is %+v; want ana, 30 days, HttpOnly", c)
	}

	// The link of another affiliate, opened later, replaces the cookie.
	second := open(t, router, "/r/bruno", c)
	if got := cookieOf(second); got == nil || got.Value != "bruno" {
		t.Fatalf("the second link's cookie is %+v; want bruno", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/cadastro", nil)
	req.AddCookie(cookieOf(second))
	if code := w.module.CodeFrom(req); code != "bruno" {
		t.Fatalf("the sign-up reads %q from the cookie, want bruno", code)
	}

	// A link to another site is not followed, and a code nobody owns lands
	// with no cookie.
	if loc := open(t, router, "/r/ana?to=//evil.example/x").Header().Get("Location"); loc != "/" {
		t.Fatalf("a protocol-relative landing was followed to %q", loc)
	}
	if loc := open(t, router, "/r/ana?to=https://evil.example").Header().Get("Location"); loc != "/" {
		t.Fatalf("a full address was followed to %q", loc)
	}
	ghost := open(t, router, "/r/ninguem")
	if cookieOf(ghost) != nil || ghost.Header().Get("Location") != "/" {
		t.Fatalf("a code nobody owns set %+v and landed on %q", cookieOf(ghost), ghost.Header().Get("Location"))
	}

	stats, err := w.svc.StatsOf(context.Background(), admin(), enroll(t, w, "user-caio", "caio", cpfCaio).ID, "2000-01-01", "2100-01-01")
	if err != nil || stats.Clicks != 0 {
		t.Fatalf("a fresh affiliate counts %d clicks, %v", stats.Clicks, err)
	}
	mine, _, _ := w.svc.AffiliatesPage(context.Background(), admin(), affiliate.AffiliatesQuery{Search: "ana"})
	if len(mine) != 1 {
		t.Fatalf("searching ana found %d", len(mine))
	}
	if stats, _ := w.svc.StatsOf(context.Background(), admin(), mine[0].ID, "2000-01-01", "2100-01-01"); stats.Clicks != 3 {
		t.Fatalf("ana's link counts %d clicks, want 3", stats.Clicks)
	}
}

func TestTheReferralIsForLife(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	enroll(t, w, "user-bruno", "bruno", cpfBruno)
	planOf(t, w, affiliate.Linear, 10000, 0, 1000)

	first := signUp(t, w, "buyer-1", "ana", "")
	if first == nil || first.AffiliateID != ana.ID {
		t.Fatalf("the sign-up was referred to %+v, want ana", first)
	}
	// A later cookie of another affiliate changes nothing: the link is for life.
	again := signUp(t, w, "buyer-1", "bruno", "")
	if again.ID != first.ID || again.AffiliateID != ana.ID {
		t.Fatalf("a later code moved the referral to %s", again.AffiliateID)
	}
	paid := pay(t, w, "order-1", "buyer-1", 10000, false)
	if len(paid) != 1 || paid[0].AffiliateID != ana.ID || paid[0].Amount != 1000 {
		t.Fatalf("the payment paid %+v; want 10%% to ana", paid)
	}
	// No code, no referral: the sign-up goes on.
	if r := signUp(t, w, "buyer-2", "", ""); r != nil {
		t.Fatalf("a sign-up with no code was referred: %+v", r)
	}
}

func TestEveryRenewalPaysLikeTheFirstPurchase(t *testing.T) {
	t.Parallel()
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	planOf(t, w, affiliate.Linear, 10000, 0, 150)
	signUp(t, w, "buyer-1", "ana", "")

	pay(t, w, "order-1", "buyer-1", 19900, false)
	pay(t, w, "order-2", "buyer-1", 19900, true)
	pay(t, w, "order-3", "buyer-1", 19900, true)
	if got := balance(t, w, ana); got != 3*299 {
		t.Fatalf("ana holds %d after a purchase and two renewals, want %d", got, 3*299)
	}
	// The same payment reported twice pays once.
	if again := pay(t, w, "order-3", "buyer-1", 19900, true); len(again) != 1 {
		t.Fatalf("reporting order-3 again answered %d commissions", len(again))
	}
	if got := balance(t, w, ana); got != 3*299 {
		t.Fatalf("reporting a payment twice moved the balance to %d", got)
	}
}

func TestEachLevelEarnsItsRateAndABlockedLevelIsSkipped(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	// ana <- bruno <- caio <- dora <- buyer: four levels above the buyer.
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	signUp(t, w, "user-bruno", "ana", cpfBruno)
	bruno := enroll(t, w, "user-bruno", "bruno", cpfBruno)
	signUp(t, w, "user-caio", "bruno", cpfCaio)
	caio := enroll(t, w, "user-caio", "caio", cpfCaio)
	signUp(t, w, "user-dora", "caio", cpfDora)
	dora := enroll(t, w, "user-dora", "dora", cpfDora)
	signUp(t, w, "buyer-1", "dora", cpfEva)
	planOf(t, w, affiliate.Multilevel, 10000, 0, 500, 200, 100, 50)

	upline, err := w.svc.Upline(ctx, admin(), "buyer-1")
	if err != nil || len(upline) != 4 || upline[0].ID != dora.ID || upline[3].ID != ana.ID {
		t.Fatalf("the buyer's upline is %d long, %v", len(upline), err)
	}

	paid := pay(t, w, "order-1", "buyer-1", 100000, false)
	want := map[string]int64{dora.ID: 5000, caio.ID: 2000, bruno.ID: 1000, ana.ID: 500}
	if len(paid) != 4 {
		t.Fatalf("a four-level plan paid %d levels", len(paid))
	}
	for _, c := range paid {
		if want[c.AffiliateID] != c.Amount {
			t.Errorf("level %d paid %d, want %d", c.Level, c.Amount, want[c.AffiliateID])
		}
	}

	// Blocking caio skips his level; the levels above still earn.
	if _, err := w.svc.SetBlocked(ctx, admin(), caio.ID, true); err != nil {
		t.Fatalf("blocking caio: %v", err)
	}
	paid = pay(t, w, "order-2", "buyer-1", 100000, true)
	if len(paid) != 3 {
		t.Fatalf("with caio blocked the payment paid %d levels, want 3", len(paid))
	}
	for _, c := range paid {
		if c.AffiliateID == caio.ID {
			t.Fatal("a blocked affiliate earned")
		}
	}

	// A linear plan pays the referrer alone.
	planOf(t, w, affiliate.Linear, 10000, 0, 300)
	if paid = pay(t, w, "order-3", "buyer-1", 100000, true); len(paid) != 1 || paid[0].AffiliateID != dora.ID || paid[0].Amount != 3000 {
		t.Fatalf("a linear plan paid %+v", paid)
	}

	tree, err := w.svc.Tree(ctx, admin(), ana.ID)
	if err != nil || len(tree) != 4 || tree[3].UserID != "buyer-1" || tree[3].Level != 4 {
		t.Fatalf("ana's tree is %+v, %v", tree, err)
	}
}

func TestTheTreeRefusesSelfReferralAndOnePersonTwice(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	enroll(t, w, "user-ana", "ana", cpfAna)
	signUp(t, w, "user-bruno", "ana", cpfBruno)
	enroll(t, w, "user-bruno", "bruno", cpfBruno)

	var errs validation.Errors
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "user-ana", Code: "ana"}); !errors.As(err, &errs) || len(errs["code"]) == 0 {
		t.Fatalf("an affiliate referred themselves, or was refused for another reason: %v", err)
	}
	// Ana's document under another account, below bruno, would close the tree.
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "ana-again", Code: "bruno", Document: cpfAna}); !errors.Is(err, affiliate.ErrSameTree) && !errors.As(err, &errs) {
		t.Fatalf("one person entered the tree twice: %v", err)
	}
	// A document already referred is refused under a second account.
	signUp(t, w, "buyer-1", "ana", cpfCaio)
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-2", Code: "bruno", Document: cpfCaio}); !errors.As(err, &errs) {
		t.Fatalf("a document was referred twice: %v", err)
	}
}

// withdrawable gives ana a payout account and R$ 500,00 of commissions.
func withdrawable(t *testing.T, w world, min, max int64) (*affiliate.Affiliate, *wallet.PayoutAccount) {
	t.Helper()
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	planOf(t, w, affiliate.Linear, min, max, 1000)
	signUp(t, w, "buyer-1", "ana", "")
	pay(t, w, "order-1", "buyer-1", 500000, false)
	account, err := w.svc.AddPayoutAccount(context.Background(), user("user-ana"), affiliate.AccountRequest{
		Kind: "pix", Details: map[string]string{"key_type": "email", "key": "ana@example.com"},
	})
	if err != nil {
		t.Fatalf("declaring the account: %v", err)
	}
	if account.HolderDocument != cpfAna {
		t.Fatalf("the account is in the document %q, want ana's", account.HolderDocument)
	}
	return ana, account
}

func TestOneWithdrawalADayAndARefusedOneFreesTheDay(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	_, account := withdrawable(t, w, 10000, 0)

	first, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{Amount: 15000, AccountID: account.ID})
	if err != nil {
		t.Fatalf("the first withdrawal of the day: %v", err)
	}
	if _, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{Amount: 15000, AccountID: account.ID}); !errors.Is(err, affiliate.ErrOnePerDay) {
		var errs validation.Errors
		if !errors.As(err, &errs) || len(errs["amount"]) == 0 {
			t.Fatalf("a second withdrawal on the same day answered %v", err)
		}
	}
	if _, err := w.svc.RejectWithdrawal(ctx, admin(), first.ID, "chave de outra pessoa"); err != nil {
		t.Fatalf("rejecting: %v", err)
	}
	again, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{AccountID: account.ID})
	if err != nil {
		t.Fatalf("asking again after the day's request was rejected: %v", err)
	}
	if int64(again.Amount) != 50000 {
		t.Fatalf("a blank amount asked for %d, want the whole R$ 500,00", again.Amount)
	}

	// Asking the wallet directly, around these rules, is refused.
	ana, _ := w.svc.Mine(ctx, user("user-ana"))
	wl, _ := w.wallets.FindBySlug(ctx, admin(), ana.UserID, affiliate.WalletSlug)
	if _, err := w.wallets.RequestWithdrawal(ctx, user("user-ana"), wallet.WithdrawalRequestInput{
		IdempotencyKey: "around", WalletID: wl.ID, Amount: "1.00", AccountID: account.ID,
	}); !errors.Is(err, wallet.ErrWithdrawalElsewhere) {
		t.Fatalf("the affiliate asked the wallet directly: %v", err)
	}

	// The operator's queue holds it, and paying it closes it with the receipt.
	queue, err := w.svc.Withdrawals(ctx, admin(), wallet.WithdrawalRequested)
	if err != nil || len(queue) != 1 || queue[0].Request.ID != again.ID {
		t.Fatalf("the queue holds %d, %v", len(queue), err)
	}
	if _, err := w.svc.StartPaying(ctx, admin(), again.ID); err != nil {
		t.Fatalf("taking it: %v", err)
	}
	if _, err := w.svc.PayWithdrawal(ctx, admin(), wallet.PaymentInput{
		RequestID: again.ID, EndToEndID: "E0000000020261012", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF pix")); err != nil {
		t.Fatalf("paying: %v", err)
	}
	if _, _, err := w.svc.WithdrawalReceipt(ctx, user("user-ana"), again.ID); err != nil {
		t.Fatalf("ana opening her receipt: %v", err)
	}
	if _, _, err := w.svc.WithdrawalReceipt(ctx, user("user-bruno"), again.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else opened ana's receipt: %v", err)
	}
}

func TestTwoWithdrawalsAskedAtOnceMakeOne(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	_, account := withdrawable(t, w, 1000, 0)

	// Eight requests of the same day at once, each small enough for the
	// balance: the day is one, so exactly one is made.
	const attempts = 8
	results := make(chan error, attempts)
	start := make(chan struct{})
	for range attempts {
		go func() {
			<-start
			_, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{Amount: 1000, AccountID: account.ID})
			results <- err
		}()
	}
	close(start)
	made := 0
	for range attempts {
		if err := <-results; err == nil {
			made++
		}
	}
	if made != 1 {
		t.Fatalf("%d withdrawals were made on one day, want 1", made)
	}
	ana, _ := w.svc.Mine(ctx, user("user-ana"))
	if b, _ := w.svc.BalanceOf(ctx, admin(), ana.ID); b.Held != 1000 {
		t.Fatalf("the wallet holds %d aside, want one request's 1000", b.Held)
	}
}

func TestAWithdrawalStaysBetweenThePlansMinimumAndMaximum(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	_, account := withdrawable(t, w, 10000, 20000)

	var errs validation.Errors
	for _, amount := range []int64{9999, 20001, 60000} {
		if _, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{Amount: amount, AccountID: account.ID}); !errors.As(err, &errs) {
			t.Errorf("a withdrawal of %d answered %v, want it refused", amount, err)
		}
	}
	// A blank amount asks for everything available, up to the maximum.
	r, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{AccountID: account.ID})
	if err != nil || int64(r.Amount) != 20000 {
		t.Fatalf("a blank amount asked for %v, %v; want the maximum", r, err)
	}
}

func TestARefundReversesTheCommissionsEvenBelowZero(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, account := withdrawable(t, w, 10000, 0)

	// Ana withdraws everything, and the payment is refunded afterwards.
	r, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{AccountID: account.ID})
	if err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	if _, err := w.svc.StartPaying(ctx, admin(), r.ID); err != nil {
		t.Fatalf("taking it: %v", err)
	}
	if _, err := w.svc.PayWithdrawal(ctx, admin(), wallet.PaymentInput{
		RequestID: r.ID, EndToEndID: "E1", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF")); err != nil {
		t.Fatalf("paying: %v", err)
	}
	conversion, err := w.svc.RecordRefund(ctx, admin(), "order-1", "chargeback")
	if err != nil || conversion.Status != affiliate.ConversionRefunded {
		t.Fatalf("refunding: %v, %v", conversion, err)
	}
	if got := balance(t, w, ana); got != -50000 {
		t.Fatalf("after the refund ana holds %d, want -50000", got)
	}
	if again, err := w.svc.RecordRefund(ctx, admin(), "order-1", "again"); err != nil || again.Status != affiliate.ConversionRefunded || balance(t, w, ana) != -50000 {
		t.Fatalf("refunding twice moved money: %v", err)
	}

	// Below zero, withdrawals are closed; new commissions pay the difference.
	if _, err := w.svc.RequestWithdrawal(ctx, user("user-ana"), affiliate.WithdrawalRequest{Amount: 10000, AccountID: account.ID}); err == nil {
		t.Fatal("a withdrawal was asked for with the balance below zero")
	}
	pay(t, w, "order-2", "buyer-1", 200000, true)
	if got := balance(t, w, ana); got != -30000 {
		t.Fatalf("a new commission left ana at %d, want -30000", got)
	}

	// The admin forgives the rest, with a reason.
	forgiven, err := w.svc.WriteOff(ctx, admin(), ana.ID, "acordo comercial")
	if err != nil || forgiven != 30000 || balance(t, w, ana) != 0 {
		t.Fatalf("the write-off forgave %d and left %d, %v", forgiven, balance(t, w, ana), err)
	}
	if again, err := w.svc.WriteOff(ctx, admin(), ana.ID, "de novo"); err != nil || again != 0 {
		t.Fatalf("a second write-off forgave %d, %v", again, err)
	}

	// A manual reversal by the admin, with its reason on the commission.
	paid := pay(t, w, "order-3", "buyer-1", 100000, true)
	reversed, err := w.svc.ReverseCommission(ctx, admin(), paid[0].ID, "venda de teste")
	if err != nil || reversed.Status != affiliate.CommissionReversed || reversed.ReversedReason != "venda de teste" {
		t.Fatalf("reversing by hand: %+v, %v", reversed, err)
	}
	rows, total, err := w.svc.CommissionsPage(ctx, user("user-ana"), affiliate.CommissionsQuery{Status: affiliate.CommissionReversed})
	if err != nil || total != 2 || len(rows) != 2 || rows[0].Conversion == nil {
		t.Fatalf("ana's reversed commissions are %d, %v", total, err)
	}
}

func TestATenantSeesNothingOfAnother(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	planOf(t, w, affiliate.Linear, 10000, 0, 150)

	other := security.Subject{ID: "finance-2", Tenant: "globex", Roles: []string{affiliate.AdminRole, wallet.OperatorRole}, Verified: true}
	if _, err := w.svc.Find(ctx, other, ana.ID); !errors.Is(err, affiliate.ErrNotFound) {
		t.Fatalf("another tenant's admin found ana: %v", err)
	}
	rows, total, err := w.svc.AffiliatesPage(ctx, other, affiliate.AffiliatesQuery{})
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("another tenant's admin listed %d affiliates, %v", total, err)
	}
	// A visitor of another tenant cannot sign up under ana's code.
	if r, err := w.svc.Attribute(ctx, security.Guest("globex"), affiliate.AttributeRequest{UserID: "buyer-x", Code: "ana"}); r != nil || !errors.Is(err, affiliate.ErrNotFound) {
		t.Fatalf("a sign-up of another tenant was referred to ana: %+v, %v", r, err)
	}
	// Nor is ana's panel anybody else's.
	if _, err := w.svc.BalanceOf(ctx, user("user-bruno"), ana.ID); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("somebody else read ana's balance: %v", err)
	}
}
