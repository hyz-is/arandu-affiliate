package unit_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"

	affiliate "github.com/hyz-is/arandu-affiliate"
	wallet "github.com/hyz-is/arandu-wallet"
)

// The properties this package exists to keep are checked here against the
// code: the policy denies what no rule allows; the service authorizes before
// it constructs or runs a generated query; the tenant comes from the Grant.
// The nil handle below panics on any statement, so a refusal that comes after
// the Model fails loudly instead of passing.

// everyAction is the whole set the policy answers about.
var everyAction = []security.Action{
	affiliate.DashboardView,
	affiliate.PlansManage,
	affiliate.AffiliatesManage,
	affiliate.ReferralRecord,
}

// manageActions are the ones only the admin takes.
var manageActions = []security.Action{affiliate.PlansManage, affiliate.AffiliatesManage}

// signedIn is a person with a session and no role this package knows.
func signedIn() security.Subject {
	return security.Subject{ID: "user-1", Tenant: "acme", Roles: []string{"admin"}, Verified: true}
}

// finance is the subject the application gave the package's admin role.
func finance() security.Subject {
	return security.Subject{ID: "user-9", Tenant: "acme", Roles: []string{affiliate.AdminRole}, Verified: true}
}

func row(id, tenant, user string) affiliate.Affiliate {
	return affiliate.Affiliate{ID: id, TenantID: tenant, UserID: user, Name: "Ana", Code: "ana", Status: affiliate.AffiliateActive}
}

func TestThePolicyRefusesWhatNoRuleAllows(t *testing.T) {
	t.Parallel()

	// A role named "admin" is the application's, not this package's.
	for _, action := range manageActions {
		_, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, signedIn(), action, affiliate.Affiliate{})
		if !errors.Is(err, security.ErrForbidden) {
			t.Errorf("a subject without %s was allowed %s: %v", affiliate.AdminRole, action, err)
		}
	}
	_, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, signedIn(), affiliate.DashboardView, row("a-2", "acme", "somebody-else"))
	if !errors.Is(err, security.ErrForbidden) {
		t.Errorf("a subject read somebody else's panel: %v", err)
	}
}

func TestTheAffiliateReadsTheirOwnPanelAndNothingElse(t *testing.T) {
	t.Parallel()

	mine := row("a-1", "acme", "user-1")
	if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, signedIn(), affiliate.DashboardView, mine); err != nil {
		t.Errorf("the affiliate was refused their own panel: %v", err)
	}
	for _, action := range manageActions {
		if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, signedIn(), action, mine); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("the affiliate was allowed %s on their own row: %v", action, err)
		}
	}
}

func TestTheAdminActsAcrossItsTenantAndNoFurther(t *testing.T) {
	t.Parallel()

	for _, action := range everyAction {
		if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, finance(), action, row("a-1", "acme", "user-1")); err != nil {
			t.Errorf("the admin was refused %s in its tenant: %v", action, err)
		}
		if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, finance(), action, row("a-1", "globex", "user-1")); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("the admin was allowed %s on another tenant's affiliate: %v", action, err)
		}
	}
}

func TestAVisitorRecordsAClickAndAReferralAndNothingElse(t *testing.T) {
	t.Parallel()

	guest := security.Guest("acme")
	if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, guest, affiliate.ReferralRecord, row("a-1", "acme", "user-1")); err != nil {
		t.Fatalf("a visitor was refused recording a click on a link of their tenant: %v", err)
	}
	if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, guest, affiliate.ReferralRecord, row("a-1", "globex", "user-1")); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a visitor recorded a click on another tenant's link: %v", err)
	}
	for _, action := range []security.Action{affiliate.DashboardView, affiliate.PlansManage, affiliate.AffiliatesManage} {
		if _, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, guest, action, affiliate.Affiliate{}); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("a visitor was allowed %s: %v", action, err)
		}
	}
}

func TestThePolicyDeniesARecordOfAnotherTenant(t *testing.T) {
	t.Parallel()

	err := affiliate.AffiliatePolicy{}.Can(context.Background(), signedIn(), affiliate.DashboardView, row("a-1", "globex", "user-1"))
	if err == nil || !strings.Contains(err.Error(), "another tenant") {
		t.Fatalf("the refusal of another tenant's record is %v", err)
	}
}

func TestAuthorizeRefusesASubjectThatIsNobody(t *testing.T) {
	t.Parallel()

	_, err := security.Authorize(context.Background(), affiliate.AffiliatePolicy{}, security.Subject{}, affiliate.DashboardView, affiliate.Affiliate{})
	if !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("an empty subject was authorized: %v", err)
	}
}

// nilHandle is a handle over no database: any statement panics.
func nilHandle() *data.DB { return data.Wrap(nil, data.DialectSQLite) }

func TestTheServiceRefusesBeforeReachingTheModel(t *testing.T) {
	t.Parallel()

	service := affiliate.NewAffiliateService(nil)
	ctx := context.Background()
	guest := security.Guest("acme")

	checks := map[string]func() error{
		"Find":         func() error { _, err := service.Find(ctx, signedIn(), "a-1"); return err },
		"Mine":         func() error { _, err := service.Mine(ctx, guest); return err },
		"Create":       func() error { _, err := service.Create(ctx, signedIn(), validAffiliate()); return err },
		"SetBlocked":   func() error { _, err := service.SetBlocked(ctx, signedIn(), "a-1", true); return err },
		"Plans":        func() error { _, err := service.Plans(ctx, signedIn()); return err },
		"ActivatePlan": func() error { _, err := service.ActivatePlan(ctx, signedIn(), "p-1"); return err },
		"ActivePlan":   func() error { _, err := service.ActivePlan(ctx, guest); return err },
		"Tree":         func() error { _, err := service.Tree(ctx, signedIn(), "a-1"); return err },
		"Withdrawals":  func() error { _, err := service.Withdrawals(ctx, signedIn(), ""); return err },
		"WriteOff":     func() error { _, err := service.WriteOff(ctx, signedIn(), "a-1", "perdão"); return err },
		"RecordRefund": func() error { _, err := service.RecordRefund(ctx, signedIn(), "o-1", ""); return err },
		"BalanceOf":    func() error { _, err := service.BalanceOf(ctx, guest, "a-1"); return err },
		"StatsOf":      func() error { _, err := service.StatsOf(ctx, guest, "a-1", "2026-10-01", "2026-10-31"); return err },
		"AffiliatesPage": func() error {
			_, _, err := service.AffiliatesPage(ctx, signedIn(), affiliate.AffiliatesQuery{})
			return err
		},
		"CommissionsPage": func() error {
			_, _, err := service.CommissionsPage(ctx, guest, affiliate.CommissionsQuery{})
			return err
		},
		"RecordConversion": func() error {
			_, _, err := service.RecordConversion(ctx, signedIn(), affiliate.ConversionRequest{
				OrderRef: "o-1", BuyerUserID: "u-1", Amount: 100, OccurredAt: time.Now(),
			})
			return err
		},
		"CreatePlan": func() error {
			_, err := service.CreatePlan(ctx, signedIn(), affiliate.PlanRequest{Name: "Plano", Kind: affiliate.Linear, Rates: []int64{150}}, true)
			return err
		},
		"RequestWithdrawal": func() error {
			_, err := service.RequestWithdrawal(ctx, guest, affiliate.WithdrawalRequest{AccountID: "acc-1"})
			return err
		},
	}
	for name, check := range checks {
		if err := check(); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s reached the Model before the policy refusal: %v", name, err)
		}
	}
}

// validAffiliate is a registration every check passes.
func validAffiliate() affiliate.CreateRequest {
	return affiliate.CreateRequest{UserID: "user-1", Name: "Ana", Document: "529.982.247-25"}
}

// recordingHandle runs nothing and keeps every statement it is handed.
type recordingHandle struct {
	*data.DB
	statements []recordedStatement
}

type recordedStatement struct {
	sql      string
	bindings []any
}

func newRecordingHandle() *recordingHandle { return &recordingHandle{DB: nilHandle()} }

func (h *recordingHandle) record(sql string, bindings []any) {
	h.statements = append(h.statements, recordedStatement{sql: sql, bindings: slices.Clone(bindings)})
}

func (h *recordingHandle) Select(_ context.Context, sql string, bindings []any, _ bool) ([]query.Record, error) {
	h.record(sql, bindings)
	return nil, nil
}

func (h *recordingHandle) Insert(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

func (h *recordingHandle) Update(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Delete(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Statement(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

func (h *recordingHandle) first(t *testing.T, verb string) recordedStatement {
	t.Helper()
	for _, statement := range h.statements {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(statement.sql)), verb) {
			return statement
		}
	}
	t.Fatalf("no %s statement was issued; recorded %v", verb, h.statements)
	return recordedStatement{}
}

// TestAffiliatesReturnsAWiredTenantScopedQuery holds what affiliateTable
// declares, through what it makes the generated query do.
func TestAffiliatesReturnsAWiredTenantScopedQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	handle := newRecordingHandle()
	table := affiliate.Affiliates(handle).Base().Table()
	if table.Name() != "affiliates" {
		t.Fatalf("Affiliates table = %q, want affiliates", table.Name())
	}
	if key := table.MorphModel(handle); key.GetKeyName() != "id" || key.GetKeyType() != "string" {
		t.Fatalf("Affiliates key is %q of type %q; want id, as text", key.GetKeyName(), key.GetKeyType())
	}
	r, err := affiliate.Affiliates(handle).New()
	if err != nil {
		t.Fatalf("building a row: %v", err)
	}
	r.ID = "record-1"
	if _, err := r.Save(ctx, security.SystemGrant(affiliate.AffiliatesManage, "acme")); err != nil {
		t.Fatalf("saving through the recording handle: %v", err)
	}
	insert := handle.first(t, "insert")
	if !slices.Contains(insert.bindings, any("record-1")) || !strings.Contains(insert.sql, "tenant_id") || !slices.Contains(insert.bindings, any("acme")) {
		t.Fatalf("the insert %q %v does not write the application key and the Grant's tenant", insert.sql, insert.bindings)
	}
	if _, err := affiliate.Affiliates(handle).WhereKey("record-1").First(ctx, security.SystemGrant(affiliate.DashboardView, "acme")); err != nil {
		t.Fatalf("reading through the recording handle: %v", err)
	}
	read := handle.first(t, "select")
	if !strings.Contains(read.sql, "tenant_id") || !slices.Contains(read.bindings, any("acme")) {
		t.Fatalf("the read %q %v is not filtered by the Grant's tenant", read.sql, read.bindings)
	}
}

func TestTheMissingRecordIsTheModelsNotFound(t *testing.T) {
	t.Parallel()

	if !errors.Is(affiliate.ErrNotFound, model.ErrModelNotFound) {
		t.Fatal("ErrNotFound does not wrap model.ErrModelNotFound, so the router would answer a missing record with 500")
	}
}

func TestASystemGrantWithoutATenantReachesNothing(t *testing.T) {
	t.Parallel()

	_, err := affiliate.Affiliates(nilHandle()).WhereKey("record-1").First(context.Background(), security.SystemGrant(affiliate.DashboardView, ""))
	if !errors.Is(err, model.ErrNoTenant) {
		t.Fatalf("a system grant with no tenant returned %v, want ErrNoTenant", err)
	}
}

func TestTheRequestsValidateTheirInput(t *testing.T) {
	t.Parallel()

	if errs := validAffiliate().Validate(); errs.Any() {
		t.Fatalf("a valid affiliate was rejected: %v", errs)
	}
	for name, change := range map[string]func(*affiliate.CreateRequest){
		"a CPF with a wrong check digit": func(r *affiliate.CreateRequest) { r.Document = "529.982.247-24" },
		"no login user":                  func(r *affiliate.CreateRequest) { r.UserID = "" },
		"a code with spaces":             func(r *affiliate.CreateRequest) { r.Code = "ana maria" },
		"a code too short":               func(r *affiliate.CreateRequest) { r.Code = "an" },
	} {
		request := validAffiliate()
		change(&request)
		if errs := request.Validate(); !errs.Any() {
			t.Errorf("%s validated", name)
		}
	}
	company := validAffiliate()
	company.Document = "11.222.333/0001-81"
	if errs := company.Validate(); errs.Any() {
		t.Errorf("an affiliate with a CNPJ was rejected: %v", errs)
	}

	plans := map[string]affiliate.PlanRequest{
		"a linear plan with two levels": {Name: "P", Kind: affiliate.Linear, Rates: []int64{150, 50}},
		"a multilevel plan with five":   {Name: "P", Kind: affiliate.Multilevel, Rates: []int64{1, 1, 1, 1, 1}},
		"a rate of zero":                {Name: "P", Kind: affiliate.Linear, Rates: []int64{0}},
		"levels paying more than 100%":  {Name: "P", Kind: affiliate.Multilevel, Rates: []int64{6000, 5000}},
		"a maximum below the minimum":   {Name: "P", Kind: affiliate.Linear, Rates: []int64{150}, MinWithdrawal: 10000, MaxWithdrawal: 5000},
		"a kind nobody declared":        {Name: "P", Kind: "pyramid", Rates: []int64{150}},
	}
	for name, plan := range plans {
		if errs := plan.Validate(); !errs.Any() {
			t.Errorf("%s validated", name)
		}
	}
	if errs := (affiliate.PlanRequest{Name: "P", Kind: affiliate.Multilevel, Rates: []int64{500, 200, 100, 50}, MinWithdrawal: 10000}).Validate(); errs.Any() {
		t.Errorf("a valid four-level plan was rejected: %v", errs)
	}
}

func TestACommissionIsTheRateOfThePaymentRoundedToTheCentavo(t *testing.T) {
	t.Parallel()

	cases := []struct{ amount, rate, want int64 }{
		{19900, 150, 299}, // 1,5% of R$ 199,00 = 2,985 -> 2,99
		{10000, 150, 150}, // 1,5% of R$ 100,00
		{333, 1000, 33},   // 10% of R$ 3,33 = 0,333 -> 0,33
		{335, 1000, 34},   // 0,335 -> 0,34
		{1, 150, 0},       // a centavo pays nothing at 1,5%
		{100000, 10000, 100000},
	}
	for _, c := range cases {
		if got := affiliate.CommissionOf(c.amount, c.rate); got != c.want {
			t.Errorf("CommissionOf(%d, %d) = %d, want %d", c.amount, c.rate, got, c.want)
		}
	}
}

func TestTheConfigurationRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	wallets := testWallet(t, nilHandle())
	for name, cfg := range map[string]affiliate.Config{
		"no arandu-wallet":               {Tenant: "acme"},
		"no tenant for visitors":         {Wallet: wallets},
		"a tenant that cannot be one":    {Tenant: "Acme Corp", Wallet: wallets},
		"a relative prefix":              {Prefix: "affiliate", Tenant: "acme", Wallet: wallets},
		"a landing on another site":      {LandingPath: "//evil.example", Tenant: "acme", Wallet: wallets},
		"a page size too big":            {PageSize: affiliate.MaxPageSize + 1, Tenant: "acme", Wallet: wallets},
		"a cookie name with a semicolon": {CookieName: "a;b", Tenant: "acme", Wallet: wallets},
	} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("the configuration with %s was accepted", name)
		}
	}
	if err := (affiliate.Config{Tenant: "acme", Wallet: wallets}).Validate(); err != nil {
		t.Fatalf("a configuration with the tenant and the wallet was refused: %v", err)
	}
}

// testWallet is arandu-wallet over a handle, the one instance an application
// passes in Config.Wallet.
func testWallet(t testing.TB, db *data.DB) *wallet.WalletService {
	t.Helper()
	sessions := security.NewSessionStore([]byte("0123456789abcdef0123456789abcdef"), time.Hour, false, security.NewMemoryBackend())
	module, err := wallet.New(wallet.Config{Tenant: "acme"}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-wallet: %v", err)
	}
	return module.Service()
}
