// Package affiliate is an Arandu module for an affiliate program: referral
// links, a referral tree that is for life, commission plans of one to four
// levels, commissions paid into arandu-wallet the moment a referred user pays,
// and withdrawals by request.
//
// The files are laid out by role rather than by layer, so the whole package
// reads top to bottom:
//
//	module.go               -> registration, routes, handlers and migrations
//	config.go               -> what the application passes in
//	model.go                -> the entities
//	policy.go               -> who may do what
//	service.go              -> the affiliates
//	plan_service.go         -> the commission plans
//	referral_service.go     -> the link, the referral and the tree
//	commission_service.go   -> payments, commissions, reversals
//	withdrawal_service.go   -> balance, payout accounts and withdrawals
//	screens.go, views.go    -> what the screens show, and the files published
//
// An application registers it explicitly. There is no service provider, no
// container and no discovery.
package affiliate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/foundation"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	fview "github.com/arandu-io/framework/view"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/translation"
	"github.com/arandu-io/hesape/view"
	wallet "github.com/hyz-is/arandu-wallet"
)

// Module is what the application registers.
type Module struct {
	cfg      Config
	svc      *AffiliateService
	sessions *security.SessionStore
}

// Compile-time proof that the module honors the contracts it claims.
var (
	_ foundation.Module      = (*Module)(nil)
	_ foundation.Migratable  = (*Module)(nil)
	_ foundation.Bootable    = (*Module)(nil)
	_ foundation.Publishable = (*Module)(nil)
)

// New returns the module, or the reason it cannot be built.
func New(cfg Config, db *data.DB, sessions *security.SessionStore) (*Module, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("affiliate: New needs a database handle: this package owns eight tables, and there is no in-memory mode that would let it start without them")
	}
	if sessions == nil {
		return nil, errors.New("affiliate: New needs a session store: the panels are mounted behind RequireAuth, which reads it to admit a request and to put who is asking on it")
	}
	cfg = cfg.withDefaults()
	svc := NewAffiliateService(db)
	svc.loc = cfg.Location
	svc.wallets = cfg.Wallet
	return &Module{cfg: cfg, svc: svc, sessions: sessions}, nil
}

// Service is the use cases, for an application that calls them from its own
// code: the sign-up that attributes a referral, the place payments are
// confirmed. Every method asks the policy, so handing it out opens nothing.
func (m *Module) Service() *AffiliateService { return m.svc }

// Name is the module identifier.
func (m *Module) Name() string { return "affiliate" }

// CodeFrom is the referral code the visitor's cookie carries, for the
// application's sign-up to pass to Attribute. Empty when there is none.
func (m *Module) CodeFrom(r *stdhttp.Request) string {
	c, err := r.Cookie(m.cfg.CookieName)
	if err != nil {
		return ""
	}
	code := strings.ToLower(strings.TrimSpace(c.Value))
	if !codePattern.MatchString(code) {
		return ""
	}
	return code
}

// adminSegment is where the admin's routes sit under the prefix.
const adminSegment = "/admin"

// Routes registers the module's routes.
//
// The referral link is the one route a visitor with no session reaches, and
// it is registered on the router the application handed in, outside the
// prefix: /r/{code}. Every other route is registered on the guarded group,
// which is mounted before the first registration.
func (m *Module) Routes(r *fhttp.Router) {
	public := r
	r = r.Group("", middleware.RequireAuth(m.sessions))

	public.Action(stdhttp.MethodGet, m.cfg.ReferralPath+"/{code}", m.referral).Name("affiliate.referral")

	// The affiliate's own panel.
	p := m.cfg.Prefix
	r.Action(stdhttp.MethodGet, p+"/me", m.dashboard).Name("affiliate.me")
	r.Action(stdhttp.MethodPost, p+"/me/payout-accounts", m.storeAccount).Name("affiliate.me.payout_accounts.store")
	r.Action(stdhttp.MethodPost, p+"/me/withdrawals", m.storeWithdrawal).Name("affiliate.me.withdrawals.store")
	r.Action(stdhttp.MethodGet, p+"/withdrawals/{id}/receipt", m.receipt).Name("affiliate.withdrawals.receipt")

	// The admin's side. The policy decides who reaches it.
	admin := p + adminSegment
	r.Action(stdhttp.MethodGet, admin+"/plans", m.plans).Name("affiliate.admin.plans.index")
	r.Action(stdhttp.MethodPost, admin+"/plans", m.storePlan).Name("affiliate.admin.plans.store")
	r.Action(stdhttp.MethodPost, admin+"/plans/{id}/activation", m.activatePlan).Name("affiliate.admin.plans.activate")
	r.Action(stdhttp.MethodGet, admin+"/affiliates", m.affiliates).Name("affiliate.admin.affiliates.index")
	r.Action(stdhttp.MethodPost, admin+"/affiliates", m.storeAffiliate).Name("affiliate.admin.affiliates.store")
	r.Action(stdhttp.MethodGet, admin+"/affiliates/{id}", m.showAffiliate).Name("affiliate.admin.affiliates.show")
	r.Action(stdhttp.MethodPost, admin+"/affiliates/{id}/block", m.block).Name("affiliate.admin.affiliates.block")
	r.Action(stdhttp.MethodDelete, admin+"/affiliates/{id}/block", m.unblock).Name("affiliate.admin.affiliates.unblock")
	r.Action(stdhttp.MethodPost, admin+"/affiliates/{id}/write-off", m.writeOff).Name("affiliate.admin.affiliates.write_off")
	r.Action(stdhttp.MethodGet, admin+"/commissions", m.commissions).Name("affiliate.admin.commissions.index")
	r.Action(stdhttp.MethodPost, admin+"/commissions/{id}/reversal", m.reverseCommission).Name("affiliate.admin.commissions.reverse")
	r.Action(stdhttp.MethodPost, admin+"/conversions", m.storeConversion).Name("affiliate.admin.conversions.store")
	r.Action(stdhttp.MethodPost, admin+"/refunds", m.storeRefund).Name("affiliate.admin.refunds.store")
	r.Action(stdhttp.MethodGet, admin+"/withdrawals", m.withdrawals).Name("affiliate.admin.withdrawals.index")
	r.Action(stdhttp.MethodPost, admin+"/withdrawals/{id}/processing", m.startWithdrawal).Name("affiliate.admin.withdrawals.process")
	r.Action(stdhttp.MethodPost, admin+"/withdrawals/{id}/payment", m.payWithdrawal).Name("affiliate.admin.withdrawals.pay")
	r.Action(stdhttp.MethodPost, admin+"/withdrawals/{id}/rejection", m.rejectWithdrawal).Name("affiliate.admin.withdrawals.reject")
}

// PublishCommand is what an application runs to take ownership of the views.
const PublishCommand = "aru vendor:publish --apply"

// Boot refuses to start until every view this package renders is linked.
func (m *Module) Boot(context.Context) error {
	prefix := viewRoot + "/" + moduleDir + "/" + m.Name() + "/"
	var stray []string
	for _, path := range PublishedPaths() {
		if !strings.HasPrefix(path, prefix) {
			stray = append(stray, path)
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("affiliate: %s would be published outside %s, where it lands on a file the application wrote",
			strings.Join(stray, ", "), prefix)
	}
	registered := make(map[string]bool)
	for _, name := range view.Registered() {
		registered[name] = true
	}
	var missing []string
	for _, name := range ViewNames() {
		if !registered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		imports := make([]string, 0, len(ViewPackages()))
		for _, pkg := range ViewPackages() {
			imports = append(imports, "<module path>/"+pkg)
		}
		return fmt.Errorf("affiliate: no view is registered as %s. Run `%s`, then `aru view:build`, then import %s in bootstrap/app.go",
			strings.Join(missing, ", "), PublishCommand, strings.Join(imports, ", "))
	}
	return nil
}

// Handlers are thin: read the input, ask the service, answer. Who is asking
// comes from ctx.User. An error from the service is returned as it is, and
// the router answers it: validation goes back with the messages in the flash,
// ErrNotFound is 404, a policy refusal is 403.

// screen is what every page of this package is drawn with.
func (m *Module) screen(ctx *fhttp.Context, who security.Subject, titleKey string) Screen {
	labels := m.Labels(translation.Locale(ctx.Ctx()))
	page := fview.New(ctx, labels.T(titleKey))
	page.Authenticated = who.ID != ""
	return Screen{Page: page, Labels: labels, Prefix: m.cfg.Prefix}
}

// today is the day it is in the application's timezone.
func (m *Module) today() Day { return DayOf(time.Now(), m.cfg.Location) }

// link is the affiliate's referral link, on the host the request came to.
func (m *Module) link(ctx *fhttp.Context, code string) string {
	scheme := "https"
	if ctx.Request.TLS == nil && !m.cfg.SecureCookie {
		scheme = "http"
	}
	return scheme + "://" + ctx.Request.Host + m.cfg.ReferralPath + "/" + url.PathEscape(code)
}

// landing is where the referral link sends the visitor: ?to= when it is a
// path of this site, the configured landing otherwise. A full address or a
// protocol-relative one is never followed, so the link cannot be turned into
// a redirect to another site.
func (m *Module) landing(to string) string {
	if to == "" || to[0] != '/' || strings.HasPrefix(to, "//") || strings.HasPrefix(to, "/\\") || strings.ContainsAny(to, "\r\n") {
		return m.cfg.LandingPath
	}
	return to
}

// referral records a click on a referral link, sets the cookie that carries
// the code to the sign-up, and lands the visitor. A code that is no active
// affiliate's lands with no cookie.
func (m *Module) referral(ctx *fhttp.Context) error {
	who, ok := ctx.User()
	if !ok {
		who = security.Guest(m.cfg.Tenant)
	}
	to := m.landing(ctx.Query("to"))
	address, _, err := net.SplitHostPort(ctx.Request.RemoteAddr)
	if err != nil {
		address = ctx.Request.RemoteAddr
	}
	owner, days, err := m.svc.RecordClick(ctx.Ctx(), who, ClickRequest{Code: ctx.Param("code"), LandingURL: to, Address: address})
	if err != nil {
		return err
	}
	if owner != nil {
		stdhttp.SetCookie(ctx.Response, &stdhttp.Cookie{
			Name: m.cfg.CookieName, Value: owner.Code, Path: "/",
			MaxAge: days * 24 * 60 * 60, HttpOnly: true, Secure: m.cfg.SecureCookie, SameSite: stdhttp.SameSiteLaxMode,
		})
	}
	return ctx.Redirect(to)
}

// period is the panel's period: ?from=&to=, the current month by default.
func (m *Module) period(ctx *fhttp.Context) (Day, Day) {
	today := m.today()
	from, errFrom := ParseDay(ctx.Query("from"))
	to, errTo := ParseDay(ctx.Query("to"))
	if errFrom != nil || from.IsZero() {
		t := today.Time()
		from = Day(time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Format(DayLayout))
	}
	if errTo != nil || to.IsZero() {
		to = today
	}
	return from, to
}

// commissionsQuery reads a list's filters and page off the request.
func (m *Module) commissionsQuery(ctx *fhttp.Context) CommissionsQuery {
	level, _ := strconv.Atoi(ctx.Query("level"))
	page, _ := strconv.Atoi(ctx.Query("page"))
	return CommissionsQuery{
		AffiliateID: ctx.Query("affiliate"), Level: level, Status: CommissionStatus(ctx.Query("status")),
		Sort: ctx.Query("sort"), Dir: ctx.Query("dir"), Page: max(page, 1), PerPage: m.cfg.PageSize,
	}
}

// dashboard is the affiliate's panel: the link, the plan, the period's
// numbers, the balance, the commissions a page at a time, the accounts and
// the withdrawals.
func (m *Module) dashboard(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	me, err := m.svc.Mine(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	from, to := m.period(ctx)
	stats, err := m.svc.StatsOf(ctx.Ctx(), who, me.ID, from, to)
	if err != nil {
		return err
	}
	balance, err := m.svc.BalanceOf(ctx.Ctx(), who, me.ID)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{
			"id": me.ID, "code": me.Code, "link": m.link(ctx, me.Code), "status": string(me.Status),
			"balance": balance.Balance, "held": balance.Held, "available": balance.Available,
			"period": map[string]any{"from": string(from), "to": string(to), "clicks": stats.Clicks,
				"signups": stats.Signups, "sales": stats.Sales, "commissions": stats.Commissions},
		})
	}
	plan, err := m.svc.ActivePlan(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	q := m.commissionsQuery(ctx)
	rows, total, err := m.svc.CommissionsPage(ctx.Ctx(), who, q)
	if err != nil {
		return err
	}
	accounts, err := m.svc.PayoutAccounts(ctx.Ctx(), who, me.ID)
	if err != nil {
		return err
	}
	requests, err := m.svc.MyWithdrawals(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	sc := m.screen(ctx, who, "screen.dashboard_title")
	data := DashboardPageData{
		Screen: sc, Name: me.Name, Code: me.Code, Link: m.link(ctx, me.Code), Blocked: !me.IsActive(),
		From: string(from), To: string(to),
		Clicks: stats.Clicks, Signups: stats.Signups, Sales: stats.Sales, Commissions: brl(stats.Commissions),
		Balance: brl(balance.Balance), Held: brl(balance.Held), Available: brl(balance.Available), Negative: balance.Balance < 0,
		Rows:  commissionRows(m.cfg.Prefix, sc.Labels, rows, m.cfg.Location),
		Level: ctx.Query("level"), Status: ctx.Query("status"),
		Table: TableState{URL: m.cfg.Prefix + "/me", Sort: q.Sort, Dir: q.Dir, CurrentPage: q.Page,
			Pages: pages(total, m.cfg.PageSize), Total: total},
		WithdrawURL: m.cfg.Prefix + "/me/withdrawals", AccountURL: m.cfg.Prefix + "/me/payout-accounts",
	}
	min, max, _ := m.svc.limits(plan)
	data.Min, data.Max = brl(min), "—"
	if max > 0 {
		data.Max = brl(max)
	}
	if plan != nil {
		row := planRow(m.cfg.Prefix, sc.Labels, *plan, m.cfg.Location)
		data.Plan = &row
	}
	for _, a := range accounts {
		if a != nil && !bool(a.Disabled) {
			data.Accounts = append(data.Accounts, AccountOption{ID: a.ID, Label: a.Label})
		}
	}
	for _, r := range requests {
		data.Withdrawals = append(data.Withdrawals, withdrawalRow(m.cfg.Prefix, sc.Labels, r, nil, m.cfg.Location))
	}
	return ctx.View(ViewDashboard, data)
}

// storeAccount declares a payout account of the asking affiliate.
func (m *Module) storeAccount(ctx *fhttp.Context) error {
	in := AccountRequest{Kind: ctx.Input("kind"), Label: ctx.Input("label"), Details: map[string]string{
		"key_type": ctx.Input("pix_key_type"), "key": ctx.Input("pix_key"),
		"bank": ctx.Input("bank"), "agency": ctx.Input("agency"), "account": ctx.Input("account_number"), "account_type": ctx.Input("account_type"),
		"network": ctx.Input("network"), "address": ctx.Input("address"), "description": ctx.Input("description"),
	}}
	who, _ := ctx.User()
	account, err := m.svc.AddPayoutAccount(ctx.Ctx(), who, in)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + "/me")
	}
	return ctx.JSON(stdhttp.StatusCreated, fieldsResource{"id": account.ID, "kind": account.Kind, "label": account.Label})
}

// centavos reads an amount in centavos from a field, or in reais from the
// field of the same name ending in _brl, which is what a form types.
func centavos(ctx *fhttp.Context, field string) int64 {
	if raw := ctx.Input(field + "_brl"); raw != "" {
		v, _ := parseReais(raw)
		return v
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(ctx.Input(field)), 10, 64)
	return v
}

// storeWithdrawal asks for a withdrawal of the asking affiliate.
func (m *Module) storeWithdrawal(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, err := m.svc.RequestWithdrawal(ctx.Ctx(), who, WithdrawalRequest{Amount: centavos(ctx, "amount"), AccountID: ctx.Input("account_id")})
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + "/me")
	}
	return ctx.JSON(stdhttp.StatusCreated, fieldsResource{"id": req.ID, "amount": int64(req.Amount), "status": string(req.Status)})
}

// receipt streams the receipt of a paid withdrawal.
func (m *Module) receipt(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, file, err := m.svc.WithdrawalReceipt(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	defer file.Body.Close()
	header := ctx.Response.Header()
	header.Set("Content-Type", req.ReceiptType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": req.ReceiptName}))
	header.Set("Cache-Control", "private, no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	ctx.Response.WriteHeader(stdhttp.StatusOK)
	_, err = io.Copy(ctx.Response, file.Body)
	return err
}

// plans is the commission plans, with the form that defines one.
func (m *Module) plans(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	list, err := m.svc.Plans(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	sc := m.screen(ctx, who, "screen.admin_plans_title")
	rows := make([]PlanRow, 0, len(list))
	for _, p := range list {
		rows = append(rows, planRow(m.cfg.Prefix, sc.Labels, p, m.cfg.Location))
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, plansResource(list))
	}
	return ctx.View(ViewAdminPlans, AdminPlansPageData{Screen: sc, Plans: rows, StoreURL: m.cfg.Prefix + adminSegment + "/plans"})
}

// storePlan defines a plan. A form types the rates as percentages
// ("5; 2; 1") and the limits in reais; the API sends rate_bps repeated and
// centavos.
func (m *Module) storePlan(ctx *fhttp.Context) error {
	if err := ctx.Request.ParseForm(); err != nil {
		return validation.Errors{"form": {"could not be read"}}
	}
	var rates []int64
	if raw := ctx.Input("rates"); raw != "" {
		parsed, ok := parseRates(raw)
		if !ok {
			return validation.Errors{"rates": {"type one percentage per level, separated by ;"}}
		}
		rates = parsed
	}
	for _, raw := range ctx.Request.PostForm["rate_bps"] {
		v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return validation.Errors{"rate_bps": {"are whole basis points"}}
		}
		rates = append(rates, v)
	}
	days, _ := strconv.Atoi(ctx.Input("cookie_days"))
	who, _ := ctx.User()
	plan, err := m.svc.CreatePlan(ctx.Ctx(), who, PlanRequest{
		Name: ctx.Input("name"), Kind: PlanKind(ctx.Input("kind")), Rates: rates, CookieDays: days,
		MinWithdrawal: centavos(ctx, "min_withdrawal"), MaxWithdrawal: centavos(ctx, "max_withdrawal"),
	}, ctx.Input("activate") == "1" || ctx.Input("activate") == "true")
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/plans")
	}
	return ctx.JSON(stdhttp.StatusCreated, plansResource([]PlanView{*plan}))
}

// activatePlan makes a plan the one in force.
func (m *Module) activatePlan(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	plan, err := m.svc.ActivatePlan(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/plans")
	}
	return ctx.JSON(stdhttp.StatusOK, plansResource([]PlanView{*plan}))
}

// affiliates is the admin's list, paged, searched, filtered and ordered on
// the server.
func (m *Module) affiliates(ctx *fhttp.Context) error {
	page, _ := strconv.Atoi(ctx.Query("page"))
	q := AffiliatesQuery{
		Search: ctx.Query("q"), Status: AffiliateStatus(ctx.Query("status")),
		Sort: ctx.Query("sort"), Dir: ctx.Query("dir"), Page: max(page, 1), PerPage: m.cfg.PageSize,
	}
	who, _ := ctx.User()
	rows, total, err := m.svc.AffiliatesPage(ctx.Ctx(), who, q)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, affiliatesResource{rows: rows, total: total})
	}
	sc := m.screen(ctx, who, "screen.admin_affiliates_title")
	list := make([]AffiliateRow, 0, len(rows))
	for _, a := range rows {
		list = append(list, affiliateRow(m.cfg.Prefix, sc.Labels, a, m.cfg.Location))
	}
	return ctx.View(ViewAdminAffiliates, AdminAffiliatesPageData{
		Screen: sc, Rows: list, Status: ctx.Query("status"),
		Table: TableState{URL: m.cfg.Prefix + adminSegment + "/affiliates", Query: q.Search, Sort: q.Sort, Dir: q.Dir,
			CurrentPage: q.Page, Pages: pages(total, m.cfg.PageSize), Total: total},
		StoreURL: m.cfg.Prefix + adminSegment + "/affiliates",
	})
}

// storeAffiliate registers an affiliate.
func (m *Module) storeAffiliate(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	a, err := m.svc.Create(ctx.Ctx(), who, CreateRequest{
		UserID: ctx.Input("user_id"), Name: ctx.Input("name"), Document: ctx.Input("document"), Code: ctx.Input("code"),
	})
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/affiliates/" + a.ID)
	}
	return ctx.JSON(stdhttp.StatusCreated, affiliatesResource{rows: []*Affiliate{a}, total: 1})
}

// showAffiliate is one affiliate for the admin: the numbers of the month, the
// balance, and the tree.
func (m *Module) showAffiliate(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	a, err := m.svc.Find(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	from, to := m.period(ctx)
	stats, err := m.svc.StatsOf(ctx.Ctx(), who, a.ID, from, to)
	if err != nil {
		return err
	}
	balance, err := m.svc.BalanceOf(ctx.Ctx(), who, a.ID)
	if err != nil {
		return err
	}
	tree, err := m.svc.Tree(ctx.Ctx(), who, a.ID)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		nodes := make([]map[string]any, 0, len(tree))
		for _, n := range tree {
			nodes = append(nodes, map[string]any{"level": n.Level, "user_id": n.UserID, "parent_id": n.ParentID,
				"affiliate_id": n.AffiliateID, "code": n.Code, "joined_at": n.JoinedAt.UTC().Format(time.RFC3339)})
		}
		out := affiliatesResource{rows: []*Affiliate{a}, total: 1}.ToArray()
		out["balance"], out["held"], out["tree"] = balance.Balance, balance.Held, nodes
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(out))
	}
	sc := m.screen(ctx, who, "screen.admin_affiliate_title")
	base := m.cfg.Prefix + adminSegment + "/affiliates/" + a.ID
	return ctx.View(ViewAdminAffiliate, AdminAffiliatePageData{
		Screen: sc, Affiliate: affiliateRow(m.cfg.Prefix, sc.Labels, a, m.cfg.Location), Link: m.link(ctx, a.Code),
		Balance: brl(balance.Balance), Held: brl(balance.Held), Available: brl(balance.Available), Negative: balance.Balance < 0,
		Clicks: stats.Clicks, Signups: stats.Signups, Sales: stats.Sales, Commissions: brl(stats.Commissions),
		Tree:     treeRows(m.cfg.Prefix, sc.Labels, tree, m.cfg.Location),
		BlockURL: base + "/block", WriteOffURL: base + "/write-off",
		CommissionsURL: m.cfg.Prefix + adminSegment + "/commissions?affiliate=" + url.QueryEscape(a.ID),
	})
}

// block blocks an affiliate.
func (m *Module) block(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	a, err := m.svc.SetBlocked(ctx.Ctx(), who, ctx.Param("id"), true)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/affiliates/" + a.ID)
	}
	return ctx.JSON(stdhttp.StatusOK, affiliatesResource{rows: []*Affiliate{a}, total: 1})
}

// unblock unblocks an affiliate.
func (m *Module) unblock(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	a, err := m.svc.SetBlocked(ctx.Ctx(), who, ctx.Param("id"), false)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/affiliates/" + a.ID)
	}
	return ctx.JSON(stdhttp.StatusOK, affiliatesResource{rows: []*Affiliate{a}, total: 1})
}

// writeOff forgives an affiliate's negative balance.
func (m *Module) writeOff(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	amount, err := m.svc.WriteOff(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/affiliates/" + ctx.Param("id"))
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"written_off": amount})
}

// commissions is every commission, for the admin, paged on the server.
func (m *Module) commissions(ctx *fhttp.Context) error {
	q := m.commissionsQuery(ctx)
	who, _ := ctx.User()
	rows, total, err := m.svc.CommissionsPage(ctx.Ctx(), who, q)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, commissionsResource{rows: rows, total: total})
	}
	sc := m.screen(ctx, who, "screen.admin_commissions_title")
	return ctx.View(ViewAdminCommissions, AdminCommissionsPageData{
		Screen: sc, Rows: commissionRows(m.cfg.Prefix, sc.Labels, rows, m.cfg.Location),
		Level: ctx.Query("level"), Status: ctx.Query("status"), AffiliateID: q.AffiliateID,
		Table: TableState{URL: m.cfg.Prefix + adminSegment + "/commissions", Sort: q.Sort, Dir: q.Dir,
			CurrentPage: q.Page, Pages: pages(total, m.cfg.PageSize), Total: total},
	})
}

// reverseCommission takes a commission back with a reason.
func (m *Module) reverseCommission(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	c, err := m.svc.ReverseCommission(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/commissions")
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": c.ID, "status": string(c.Status)})
}

// storeConversion records a payment, by the admin's hand or the
// application's client: order_ref, buyer_user_id, product_id, amount in
// centavos (or amount_brl), renewal, occurred_at in RFC 3339.
func (m *Module) storeConversion(ctx *fhttp.Context) error {
	at := time.Now()
	if raw := ctx.Input("occurred_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return validation.Errors{"occurred_at": {"must be a date and time in RFC 3339"}}
		}
		at = parsed
	}
	who, _ := ctx.User()
	conversion, paid, err := m.svc.RecordConversion(ctx.Ctx(), who, ConversionRequest{
		OrderRef: ctx.Input("order_ref"), BuyerUserID: ctx.Input("buyer_user_id"), ProductID: ctx.Input("product_id"),
		Amount: centavos(ctx, "amount"), Renewal: ctx.Input("renewal") == "1" || ctx.Input("renewal") == "true", OccurredAt: at,
	})
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/commissions")
	}
	return ctx.JSON(stdhttp.StatusCreated, conversionResource{conversion: conversion, paid: paid})
}

// storeRefund records that a payment was given back, reversing its
// commissions.
func (m *Module) storeRefund(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	conversion, err := m.svc.RecordRefund(ctx.Ctx(), who, ctx.Input("order_ref"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/commissions")
	}
	return ctx.JSON(stdhttp.StatusOK, conversionResource{conversion: conversion})
}

// withdrawals is the operator's queue of affiliates' withdrawals.
func (m *Module) withdrawals(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	status := wallet.WithdrawalStatus(ctx.Query("status"))
	list, err := m.svc.Withdrawals(ctx.Ctx(), who, status)
	if err != nil {
		return err
	}
	sc := m.screen(ctx, who, "screen.admin_withdrawals_title")
	rows := make([]WithdrawalRow, 0, len(list))
	for _, w := range list {
		rows = append(rows, withdrawalRow(m.cfg.Prefix, sc.Labels, w.Request, w.Affiliate, m.cfg.Location))
	}
	if ctx.WantsJSON() {
		items := make([]map[string]any, 0, len(list))
		for _, w := range list {
			item := map[string]any{"id": w.Request.ID, "amount": int64(w.Request.Amount), "status": string(w.Request.Status),
				"requested_at": w.Request.RequestedAt.UTC().Format(time.RFC3339)}
			if w.Affiliate != nil {
				item["affiliate_id"] = w.Affiliate.ID
			}
			items = append(items, item)
		}
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{"items": items})
	}
	return ctx.View(ViewAdminWithdrawals, AdminWithdrawalsPageData{
		Screen: sc, Rows: rows, Status: string(status), URL: m.cfg.Prefix + adminSegment + "/withdrawals",
	})
}

// startWithdrawal takes an affiliate's withdrawal to be paid.
func (m *Module) startWithdrawal(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, err := m.svc.StartPaying(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/withdrawals")
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": req.ID, "status": string(req.Status)})
}

// maxReceiptBytes bounds the multipart form a payment is sent in.
const maxReceiptBytes = 10 << 20

// payWithdrawal records that an affiliate's withdrawal was paid, with the
// end-to-end identifier and the receipt as the part named "receipt".
func (m *Module) payWithdrawal(ctx *fhttp.Context) error {
	ctx.Request.Body = stdhttp.MaxBytesReader(ctx.Response, ctx.Request.Body, maxReceiptBytes+(1<<20))
	if err := ctx.Request.ParseMultipartForm(maxReceiptBytes); err != nil {
		return validation.Errors{"receipt": {"could not be read; send the form as multipart/form-data, under the size limit"}}
	}
	in := wallet.PaymentInput{RequestID: ctx.Param("id"), EndToEndID: strings.TrimSpace(ctx.Request.FormValue("end_to_end_id"))}
	file, header, err := ctx.Request.FormFile("receipt")
	if err != nil {
		return validation.Errors{"receipt": {"is required"}}
	}
	defer file.Close()
	in.ReceiptName, in.ReceiptType = header.Filename, header.Header.Get("Content-Type")
	who, _ := ctx.User()
	req, err := m.svc.PayWithdrawal(ctx.Ctx(), who, in, file)
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/withdrawals")
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": req.ID, "status": string(req.Status)})
}

// rejectWithdrawal refuses an affiliate's withdrawal with a reason.
func (m *Module) rejectWithdrawal(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	req, err := m.svc.RejectWithdrawal(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	if !ctx.WantsJSON() {
		return ctx.Redirect(m.cfg.Prefix + adminSegment + "/withdrawals")
	}
	return ctx.JSON(stdhttp.StatusOK, fieldsResource{"id": req.ID, "status": string(req.Status)})
}

// Migrations declares the schema this module owns, in the order the names
// sort in.
func (m *Module) Migrations() []foundation.Migration {
	return []foundation.Migration{
		createAffiliates{}, createReferrals{}, createPlans{}, createPlanLevels{},
		createClicks{}, createConversions{}, createCommissions{}, createWithdrawalDays{},
	}
}

var (
	_ migrations.ReversibleMigration = createAffiliates{}
	_ migrations.ReversibleMigration = createReferrals{}
	_ migrations.ReversibleMigration = createPlans{}
	_ migrations.ReversibleMigration = createPlanLevels{}
	_ migrations.ReversibleMigration = createClicks{}
	_ migrations.ReversibleMigration = createConversions{}
	_ migrations.ReversibleMigration = createCommissions{}
	_ migrations.ReversibleMigration = createWithdrawalDays{}
)

// createAffiliates is the affiliates.
type createAffiliates struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createAffiliates) GetName() string { return "20261013_0001_create_affiliates" }

// Up creates the table.
func (createAffiliates) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, affiliatesTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("user_id", maxIdentifierLen)
		table.String("code", maxCodeLen)
		table.String("document", maxDocumentLen)
		table.String("name", maxNameLen)
		table.String("status", maxShortLen)
		table.String("created_by", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Unique([]string{"tenant_id", "user_id"}, "affiliates_user_uq")
		table.Unique([]string{"tenant_id", "code"}, "affiliates_code_uq")
		table.Unique([]string{"tenant_id", "document"}, "affiliates_document_uq")
		table.Index([]string{"tenant_id", "created_at", "id"}, "affiliates_tenant_created_idx")
	})
}

// Down drops the table.
func (createAffiliates) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, affiliatesTable)
}

// createReferrals is the lifelong links of users to their referrers.
type createReferrals struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createReferrals) GetName() string { return "20261013_0002_create_affiliate_referrals" }

// Up creates the table.
func (createReferrals) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, referralsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("user_id", maxIdentifierLen)
		table.String("affiliate_id", maxIdentifierLen)
		// NULL when the application did not know it, so the unique index
		// below holds among the documents that are known.
		table.String("document", maxDocumentLen).Nullable()
		table.String("code", maxCodeLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Unique([]string{"tenant_id", "user_id"}, "affiliate_referrals_user_uq")
		table.Unique([]string{"tenant_id", "document"}, "affiliate_referrals_document_uq")
		table.Index([]string{"tenant_id", "affiliate_id", "created_at"}, "affiliate_referrals_affiliate_idx")
	})
}

// Down drops the table.
func (createReferrals) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, referralsTable)
}

// createPlans is the commission plans.
type createPlans struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createPlans) GetName() string { return "20261013_0003_create_affiliate_plans" }

// Up creates the table.
func (createPlans) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, plansTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("name", maxNameLen)
		table.String("kind", maxShortLen)
		table.Integer("cookie_days")
		table.BigInteger("min_withdrawal")
		table.BigInteger("max_withdrawal")
		table.UnsignedSmallInteger("active").Default(0)
		table.String("created_by", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Index([]string{"tenant_id", "active"}, "affiliate_plans_active_idx")
	})
}

// Down drops the table.
func (createPlans) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, plansTable)
}

// createPlanLevels is the rate of each level of each plan.
type createPlanLevels struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createPlanLevels) GetName() string { return "20261013_0004_create_affiliate_plan_levels" }

// Up creates the table.
func (createPlanLevels) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, planLevelsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("plan_id", maxIdentifierLen)
		table.Integer("level")
		table.BigInteger("rate_bps")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Unique([]string{"tenant_id", "plan_id", "level"}, "affiliate_plan_levels_uq")
	})
}

// Down drops the table.
func (createPlanLevels) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, planLevelsTable)
}

// createClicks is the openings of referral links.
type createClicks struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createClicks) GetName() string { return "20261013_0005_create_affiliate_clicks" }

// Up creates the table.
func (createClicks) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, clicksTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("affiliate_id", maxIdentifierLen)
		table.Timestamp("at")
		table.String("landing_url", maxURLLen)
		table.String("ip_hash", maxIdentifierLen)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Index([]string{"tenant_id", "affiliate_id", "at"}, "affiliate_clicks_affiliate_idx")
	})
}

// Down drops the table.
func (createClicks) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, clicksTable)
}

// createConversions is the payments of referred users.
type createConversions struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createConversions) GetName() string { return "20261013_0006_create_affiliate_conversions" }

// Up creates the table.
func (createConversions) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, conversionsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("order_ref", maxIdentifierLen*2)
		table.String("buyer_user_id", maxIdentifierLen)
		table.String("product_id", maxIdentifierLen)
		table.BigInteger("amount")
		table.UnsignedSmallInteger("renewal").Default(0)
		table.String("affiliate_id", maxIdentifierLen).Default("")
		table.String("plan_id", maxIdentifierLen).Default("")
		table.String("status", maxShortLen)
		table.Timestamp("occurred_at")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Unique([]string{"tenant_id", "order_ref"}, "affiliate_conversions_order_uq")
		table.Index([]string{"tenant_id", "affiliate_id", "occurred_at"}, "affiliate_conversions_affiliate_idx")
	})
}

// Down drops the table.
func (createConversions) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, conversionsTable)
}

// createCommissions is what each level earned on each payment.
type createCommissions struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createCommissions) GetName() string { return "20261013_0007_create_affiliate_commissions" }

// Up creates the table.
func (createCommissions) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, commissionsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("conversion_id", maxIdentifierLen)
		table.String("affiliate_id", maxIdentifierLen)
		table.Integer("level")
		table.BigInteger("rate_bps")
		table.BigInteger("amount")
		table.String("status", maxShortLen)
		table.String("wallet_operation_id", maxIdentifierLen).Default("")
		table.String("reversal_operation_id", maxIdentifierLen).Default("")
		table.String("reversed_reason", maxTextLen).Default("")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		// One commission per payment and level: a payment counted twice at
		// once pays once.
		table.Unique([]string{"tenant_id", "conversion_id", "level"}, "affiliate_commissions_level_uq")
		table.Index([]string{"tenant_id", "affiliate_id", "created_at"}, "affiliate_commissions_affiliate_idx")
	})
}

// Down drops the table.
func (createCommissions) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, commissionsTable)
}

// createWithdrawalDays is the one withdrawal of each affiliate's day.
type createWithdrawalDays struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createWithdrawalDays) GetName() string { return "20261013_0008_create_affiliate_withdrawal_days" }

// Up creates the table.
func (createWithdrawalDays) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, withdrawalDaysTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("affiliate_id", maxIdentifierLen)
		table.String("day", len(DayLayout))
		table.String("withdrawal_request_id", maxIdentifierLen).Default("")
		table.Integer("attempt")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Unique([]string{"tenant_id", "affiliate_id", "day"}, "affiliate_withdrawal_days_uq")
		table.Index([]string{"tenant_id", "withdrawal_request_id"}, "affiliate_withdrawal_days_request_idx")
	})
}

// Down drops the table.
func (createWithdrawalDays) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, withdrawalDaysTable)
}
