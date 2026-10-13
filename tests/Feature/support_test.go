package feature_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	hedatabase "github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/filesystem"

	affiliate "github.com/hyz-is/arandu-affiliate"
	wallet "github.com/hyz-is/arandu-wallet"
)

// dsn opens a database file the way the tests need it: the write lock taken
// when a transaction begins, readers beside a writer, and a wait instead of an
// error when the lock is held.
const dsn = "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)"

// tenant is the customer every test runs as, except where a second one is the
// point.
const tenant = "acme"

// world is one application with this package installed over a real database.
type world struct {
	db      *data.DB
	module  *affiliate.Module
	svc     *affiliate.AffiliateService
	wallets *wallet.WalletService
}

// install opens a fresh SQLite database, applies arandu-wallet's migrations
// and this package's, and builds both modules over it.
func install(t *testing.T) world {
	t.Helper()

	path := filepath.Join(t.TempDir(), "affiliate.sqlite")
	handle, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+dsn)
	if err != nil {
		t.Fatalf("opening SQLite: %v", err)
	}
	handle.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = handle.Close() })

	connection := hedatabase.NewConnection(handle, path, "", map[string]any{
		"driver": string(hedatabase.DialectSQLite),
		"name":   "affiliate-test",
	})
	db := data.Wrap(handle, data.DialectSQLite)
	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	walletModule, err := wallet.New(wallet.Config{Tenant: tenant, Receipts: testDisk(t)}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-wallet: %v", err)
	}
	module, err := affiliate.New(affiliate.Config{Tenant: tenant, Wallet: walletModule.Service()}, db, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	migrations := hedatabase.ForMigrations(connection)
	for _, migration := range append(walletModule.Migrations(), module.Migrations()...) {
		if err := migration.Up(context.Background(), migrations); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	return world{db: db, module: module, svc: module.Service(), wallets: walletModule.Service()}
}

// admin is the finance team of the tenant: this package's admin, and
// arandu-wallet's operator, because a commission moves money.
func admin() security.Subject {
	return security.Subject{ID: "finance-1", Tenant: tenant, Roles: []string{affiliate.AdminRole, wallet.OperatorRole}, Verified: true}
}

// user is a signed-in person of the tenant with no role of this package.
func user(id string) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true}
}

// Valid documents, so a refusal is the rule's and not the check digit's.
const (
	cpfAna   = "52998224725"
	cpfBruno = "11144477735"
	cpfCaio  = "39053344705"
	cpfDora  = "86288366757"
	cpfEva   = "71428793860"
)

// enroll registers an affiliate with a code and fails the test if it cannot.
func enroll(t *testing.T, w world, userID, code, cpf string) *affiliate.Affiliate {
	t.Helper()
	a, err := w.svc.Create(context.Background(), admin(), affiliate.CreateRequest{UserID: userID, Name: "Afiliado " + userID, Document: cpf, Code: code})
	if err != nil {
		t.Fatalf("enrolling %s: %v", userID, err)
	}
	return a
}

// signUp attributes a new user to a code, as the application's sign-up does.
func signUp(t *testing.T, w world, userID, code, document string) *affiliate.Referral {
	t.Helper()
	r, err := w.svc.Attribute(context.Background(), security.Guest(tenant), affiliate.AttributeRequest{UserID: userID, Code: code, Document: document})
	if err != nil {
		t.Fatalf("signing %s up with %q: %v", userID, code, err)
	}
	return r
}

// planOf activates a plan with the rates given, in basis points.
func planOf(t *testing.T, w world, kind affiliate.PlanKind, min, max int64, rates ...int64) *affiliate.PlanView {
	t.Helper()
	p, err := w.svc.CreatePlan(context.Background(), admin(), affiliate.PlanRequest{
		Name: "Plano", Kind: kind, Rates: rates, MinWithdrawal: min, MaxWithdrawal: max,
	}, true)
	if err != nil {
		t.Fatalf("defining the plan: %v", err)
	}
	return p
}

// pay records a payment of a user.
func pay(t *testing.T, w world, ref, buyer string, amount int64, renewal bool) []*affiliate.Commission {
	t.Helper()
	_, paid, err := w.svc.RecordConversion(context.Background(), admin(), affiliate.ConversionRequest{
		OrderRef: ref, BuyerUserID: buyer, ProductID: "placaflow", Amount: amount, Renewal: renewal,
		OccurredAt: time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("recording %s: %v", ref, err)
	}
	return paid
}

// balance is an affiliate's wallet balance in centavos.
func balance(t *testing.T, w world, a *affiliate.Affiliate) int64 {
	t.Helper()
	b, err := w.svc.BalanceOf(context.Background(), admin(), a.ID)
	if err != nil {
		t.Fatalf("reading %s's balance: %v", a.UserID, err)
	}
	return b.Balance
}

// testDisk is a Disk over a fresh directory.
func testDisk(t testing.TB) *filesystem.Disk {
	t.Helper()
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("building a local Disk: %v", err)
	}
	return filesystem.NewDisk("receipts", adapter)
}

// testWallet is arandu-wallet over a handle, the one instance an application
// passes in Config.Wallet.
func testWallet(t testing.TB, db *data.DB) *wallet.WalletService {
	t.Helper()
	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	module, err := wallet.New(wallet.Config{Tenant: tenant}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-wallet: %v", err)
	}
	return module.Service()
}
