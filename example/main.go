//go:build example

// Command example wires this package the way an application does, and then
// runs an affiliate program: the plan, two affiliates in a tree, a sign-up
// through a link, a purchase and a renewal paid up the tree, a refund past the
// balance, and a withdrawal.
//
// Run it:
//
//	go run -tags example ./example
//
// It is behind a build tag so that installing this package never compiles it.
// It uses SQLite in a temporary directory and leaves nothing behind, so it needs
// no configuration and no server.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	hdatabase "github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/filesystem"
	br "github.com/hyz-is/arandu-br"
	wallet "github.com/hyz-is/arandu-wallet"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

// tenant is the customer this example writes as. An application reads its own
// from configuration, and never from a request.
const tenant = "acme"

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "example:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "arandu-affiliate-example")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "affiliate.db")
	pool, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+"?_txlock=immediate")
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()

	// ---- the wiring, which is what an application copies -------------------
	//
	// arandu-wallet keeps the commissions; this package keeps none. Tenant is
	// the customer a visitor with no session is read as on /r/{code}.
	key := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(key, time.Hour, false, security.NewMemoryBackend())
	handle := data.Wrap(pool, data.DialectSQLite)
	adapter, err := filesystem.NewLocalFilesystemAdapter(filepath.Join(dir, "receipts"))
	if err != nil {
		return err
	}
	walletModule, err := wallet.New(wallet.Config{Tenant: tenant, Receipts: filesystem.NewDisk("receipts", adapter)}, handle, sessions)
	if err != nil {
		return err
	}
	module, err := affiliate.New(affiliate.Config{Tenant: tenant, Wallet: walletModule.Service()}, handle, sessions)
	if err != nil {
		return err
	}

	// In an application this is `aru migrate`, run as a pipeline step.
	connection := hdatabase.NewConnection(pool, path, "", map[string]any{"driver": string(hdatabase.DialectSQLite), "name": "example"})
	for _, migration := range append(walletModule.Migrations(), module.Migrations()...) {
		if err := migration.Up(ctx, hdatabase.ForMigrations(connection)); err != nil {
			return fmt.Errorf("applying %s: %w", migration.GetName(), err)
		}
	}

	// Who is acting. The finance team is this package's admin and
	// arandu-wallet's operator; an affiliate needs no role.
	finance := security.Subject{ID: "finance-1", Tenant: tenant, Verified: true, Roles: []string{affiliate.AdminRole, wallet.OperatorRole}}
	svc := module.Service()

	say("plan", "two levels: 10% to whoever referred the buyer, 2% to whoever referred them")
	must(svc.CreatePlan(ctx, finance, affiliate.PlanRequest{
		Name: "Padrão", Kind: affiliate.Multilevel, Rates: []int64{1000, 200}, MinWithdrawal: 10000,
	}, true))

	say("tree", "ana is an affiliate; bruno signed up through ana's link and is an affiliate too")
	ana := must(svc.Create(ctx, finance, affiliate.CreateRequest{UserID: "user-ana", Name: "Ana", Document: "529.982.247-25", Code: "ana"}))
	must(svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "user-bruno", Code: "ana", Document: "111.444.777-35"}))
	bruno := must(svc.Create(ctx, finance, affiliate.CreateRequest{UserID: "user-bruno", Name: "Bruno", Document: "111.444.777-35", Code: "bruno"}))

	say("link", "a visitor opens bruno's link: the click is recorded and the cookie carries the code")
	router := fhttp.NewRouter()
	module.Routes(router.ForModule(module.Name()))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r/bruno?to=/planos", nil))
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == affiliate.DefaultCookieName {
			cookie = c
		}
	}
	fmt.Printf("  %d to %s, cookie %s=%s for %d days\n", rec.Code, rec.Header().Get("Location"), cookie.Name, cookie.Value, cookie.MaxAge/86400)

	say("sign-up", "the application's sign-up reads the cookie: carla is bruno's, for life")
	signUp := httptest.NewRequest(http.MethodPost, "/cadastro", nil)
	signUp.AddCookie(cookie)
	must(svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "user-carla", Code: module.CodeFrom(signUp)}))

	say("pay", "carla buys and renews: every payment pays bruno 10% and ana 2%")
	for _, p := range []affiliate.ConversionRequest{
		{OrderRef: "mp:1", BuyerUserID: "user-carla", ProductID: "placaflow", Amount: 19900, OccurredAt: time.Now()},
		{OrderRef: "mp:2", BuyerUserID: "user-carla", ProductID: "placaflow", Amount: 19900, Renewal: true, OccurredAt: time.Now()},
	} {
		_, paid, err := svc.RecordConversion(ctx, finance, p)
		if err != nil {
			return err
		}
		for _, c := range paid {
			fmt.Printf("  %s level %d: %s\n", p.OrderRef, c.Level, br.BRL(c.Amount))
		}
	}
	report(ctx, svc, finance, ana, bruno)

	say("withdraw", "bruno has R$ 39,80: below the R$ 100,00 minimum, so a bigger sale first")
	must2(svc.RecordConversion(ctx, finance, affiliate.ConversionRequest{OrderRef: "mp:3", BuyerUserID: "user-carla", ProductID: "placaflow", Amount: 100000, Renewal: true, OccurredAt: time.Now()}))
	brunoUser := security.Subject{ID: "user-bruno", Tenant: tenant, Verified: true}
	account := must(svc.AddPayoutAccount(ctx, brunoUser, affiliate.AccountRequest{
		Kind: "pix", Details: map[string]string{"key_type": "email", "key": "bruno@example.com"},
	}))
	request := must(svc.RequestWithdrawal(ctx, brunoUser, affiliate.WithdrawalRequest{AccountID: account.ID}))
	must(svc.StartPaying(ctx, finance, request.ID))
	must(svc.PayWithdrawal(ctx, finance, wallet.PaymentInput{
		RequestID: request.ID, EndToEndID: "E00000000202610131200", ReceiptName: "pix.pdf", ReceiptType: "application/pdf",
	}, strings.NewReader("%PDF-1.4 comprovante")))
	fmt.Printf("  withdrew %s\n", br.BRL(int64(request.Amount)))
	must2(svc.RecordConversion(ctx, finance, affiliate.ConversionRequest{OrderRef: "mp:4", BuyerUserID: "user-carla", ProductID: "placaflow", Amount: 100000, Renewal: true, OccurredAt: time.Now()}))
	fmt.Print("  another R$ 100,00 came in, and a second withdrawal today is refused: ")
	if _, err := svc.RequestWithdrawal(ctx, brunoUser, affiliate.WithdrawalRequest{Amount: 10000, AccountID: account.ID}); err != nil {
		fmt.Println(err)
	}

	say("refund", "both big sales are refunded, one of them already withdrawn: bruno's balance goes below zero")
	must(svc.RecordRefund(ctx, finance, "mp:3", "chargeback"))
	must(svc.RecordRefund(ctx, finance, "mp:4", "chargeback"))
	report(ctx, svc, finance, ana, bruno)

	say("write-off", "the admin forgives the difference, with a reason")
	forgiven := must(svc.WriteOff(ctx, finance, bruno.ID, "acordo comercial"))
	fmt.Printf("  forgave %s\n", br.BRL(forgiven))
	report(ctx, svc, finance, ana, bruno)
	return nil
}

// report prints each affiliate's balance.
func report(ctx context.Context, svc *affiliate.AffiliateService, actor security.Subject, people ...*affiliate.Affiliate) {
	for _, a := range people {
		b := must(svc.BalanceOf(ctx, actor, a.ID))
		fmt.Printf("  %s holds %s\n", a.Name, br.BRL(b.Balance))
	}
}

func say(step, what string) { fmt.Printf("\n%s: %s\n", step, what) }

// must stops the example at the first refusal, naming it.
func must[T any](value T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, "example:", err)
		os.Exit(1)
	}
	return value
}

// must2 is must for the calls that answer two values.
func must2[T, U any](a T, b U, err error) (T, U) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "example:", err)
		os.Exit(1)
	}
	return a, b
}
