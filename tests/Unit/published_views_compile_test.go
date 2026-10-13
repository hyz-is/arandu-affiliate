package unit_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/framework/foundation"
)

// viewCompiler is the command line whose view compiler the published views are
// held against, at the release an application builds them with.
//
// It is a version and never "latest", so this test changes its answer when this
// line changes and never because somebody published something. Moving it is
// how a stricter compiler is taken on: raise it, run the suite, and fix the
// view it refuses here rather than in every application that publishes it.
const viewCompiler = "github.com/arandu-io/aru@v0.72.2"

// TestEveryPublishedViewCompiles publishes the views into a project of their
// own, builds them with the view compiler an application runs, and compiles
// what it wrote.
//
// Nothing else in this repository reads the markup. The sources open with a
// build tag, so go build, go vet and go test never look past it, and the view
// compiler refuses what the Go compiler would accept -- a value written into an
// address behind text it cannot read, a value in a position no escape covers.
// A view it refuses is a view that stops the build of every application that
// publishes it, and without this test the first place to say so is somebody
// else's terminal.
//
// The project is laid out as an application's is once it has published: each
// file where the publication writes it, and a go.mod that resolves this package
// to the checkout under test. What the view compiler writes then goes through
// the Go compiler, which is what catches a field the markup names and the page
// data does not have.
//
// It does not skip. A gate that steps aside when its input is missing is how
// the views stopped compiling with nothing here saying so.
func TestEveryPublishedViewCompiles(t *testing.T) {
	root := packageRoot(t)
	project := t.TempDir()

	publications, err := foundation.Publications(module(t))
	if err != nil {
		t.Fatalf("reading what the module publishes: %v", err)
	}
	var published int
	for _, publication := range publications {
		from := publication.From
		if from == "" {
			from = "."
		}
		err := fs.WalkDir(publication.Files, from, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative := name
			if from != "." {
				relative = strings.TrimPrefix(strings.TrimPrefix(name, from), "/")
			}
			body, err := fs.ReadFile(publication.Files, name)
			if err != nil {
				return err
			}
			target := filepath.Join(project, filepath.FromSlash(path.Join(publication.To, relative)))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			published++
			return os.WriteFile(target, body, 0o644)
		})
		if err != nil {
			t.Fatalf("publishing the %s files: %v", publication.Tag, err)
		}
	}
	if published == 0 {
		t.Fatal("the module publishes no file, so this gate would compile nothing")
	}

	// The application's go.mod names this package and points it at the
	// checkout, so the views compile against the code beside them rather than
	// against a release. The sums come from here: the application needs no
	// module this package does not already require.
	goMod := readReleaseFile(t, root, "go.mod")
	language := captureReleaseValue(t, goMod, `(?m)^go ([0-9.]+)$`, "go directive in go.mod")
	modulePath := captureReleaseValue(t, goMod, `(?m)^module (\S+)$`, "module path in go.mod")
	manifest := "module example.com/application\n\n" +
		"go " + language + "\n\n" +
		"require " + modulePath + " v0.0.0-00010101000000-000000000000\n\n" +
		"replace " + modulePath + " => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.sum"), []byte(readReleaseFile(t, root, "go.sum")), 0o644); err != nil {
		t.Fatal(err)
	}

	inProject(t, project, "the published views do not compile with "+viewCompiler+", so no application that publishes them can build",
		"go", "run", viewCompiler, "view:build")
	inProject(t, project, "the views "+viewCompiler+" compiled do not build against this package",
		"go", "build", "./...")

	// Compiling is not drawing. Each screen is rendered once, with a row of
	// every kind and every action switched on, under a layout that draws the
	// content section alone -- so a page that compiles and then fails on its
	// own data, or draws a key instead of a sentence, fails here.
	probe := filepath.Join(project, "renderprobe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(renderProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	inProject(t, project, "a published view does not render the page data its handler hands it",
		"go", "run", "./renderprobe")
}

// renderProbe renders every screen with sample data and checks what it drew.
const renderProbe = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/arandu-io/hesape/view"
	affiliate "github.com/hyz-is/arandu-affiliate"

	_ "example.com/application/storage/framework/views/modules/affiliate"
	_ "example.com/application/storage/framework/views/modules/affiliate/admin"
)

func main() {
	view.RegisterLayout("layouts.app", func(w io.Writer, _ any, sections map[string]func(io.Writer) error) error {
		return sections["content"](w)
	})
	sc := affiliate.Screen{Labels: affiliate.Labels{Locale: "pt-BR"}, Prefix: "/afiliado"}
	plan := affiliate.PlanRow{ID: "p1", Name: "Padrão", Kind: "Multinível", KindKey: "multilevel", Min: "R$ 100,00", Max: "—",
		CookieDays: 30, Active: true, Levels: []affiliate.LevelRow{{Level: 1, Rate: "5,00%"}, {Level: 2, Rate: "2,00%"}},
		ActivateURL: "/afiliado/admin/plans/p1/activation"}
	idle := plan
	idle.ID, idle.Active, idle.ActivateURL = "p2", false, "/afiliado/admin/plans/p2/activation"
	commission := affiliate.CommissionRow{ID: "c1", Date: "12/10/2026 14:30", Level: 1, Rate: "5,00%", Amount: "R$ 9,95",
		Status: "Disponível", StatusKey: "available", Order: "pedido-1", Buyer: "u-1", Product: "placaflow",
		Affiliate: "Ana", AffiliateURL: "/afiliado/admin/affiliates/a1", Reversible: true, ReverseURL: "/afiliado/admin/commissions/c1/reversal"}
	withdrawal := affiliate.WithdrawalRow{ID: "w1", Affiliate: "Ana", Amount: "R$ 150,00", Account: "PIX ana@example.com",
		Status: "Pedido", StatusKey: "requested", RequestedAt: "12/10/2026 10:00", Open: true,
		ProcessURL: "/afiliado/admin/withdrawals/w1/processing", PayURL: "/afiliado/admin/withdrawals/w1/payment",
		RejectURL: "/afiliado/admin/withdrawals/w1/rejection"}
	paying := withdrawal
	paying.ID, paying.Open, paying.Processing, paying.StatusKey = "w2", false, true, "processing"
	paying.PayURL = "/afiliado/admin/withdrawals/w2/payment"
	row := affiliate.AffiliateRow{ID: "a1", Name: "Ana Souza", Code: "ana", Document: "529.982.247-25", User: "user-ana",
		Status: "Ativo", StatusKey: "active", Active: true, Created: "01/10/2026 09:00", URL: "/afiliado/admin/affiliates/a1"}
	table := affiliate.TableState{URL: "/afiliado/admin/commissions", CurrentPage: 1, Pages: 3, Total: 61}
	pages := []struct {
		name string
		data any
		want []string
	}{
		{affiliate.ViewDashboard, affiliate.DashboardPageData{Screen: sc, Name: "Ana Souza", Code: "ana", Link: "https://app.example/r/ana",
			Plan: &plan, From: "2026-10-01", To: "2026-10-12", Clicks: 42, Signups: 7, Sales: 3, Commissions: "R$ 29,85",
			Balance: "R$ 300,00", Held: "R$ 0,00", Available: "R$ 300,00", Negative: true, Min: "R$ 100,00", Max: "—",
			Rows: []affiliate.CommissionRow{commission}, Table: affiliate.TableState{URL: "/afiliado/me", CurrentPage: 1, Pages: 1, Total: 1},
			Accounts: []affiliate.AccountOption{{ID: "acc-1", Label: "PIX ana@example.com"}},
			Withdrawals: []affiliate.WithdrawalRow{withdrawal}, WithdrawURL: "/afiliado/me/withdrawals", AccountURL: "/afiliado/me/payout-accounts"},
			[]string{"Seu link de indicação", "https://app.example/r/ana", "Copiar", "R$ 9,95", "Nível 1", "5,00%", "/afiliado/me/withdrawals", "Chave PIX", "saldo está negativo", "42"}},
		{affiliate.ViewAdminPlans, affiliate.AdminPlansPageData{Screen: sc, Plans: []affiliate.PlanRow{plan, idle}, StoreURL: "/afiliado/admin/plans"},
			[]string{"Planos de comissão", "Padrão", "1º 5,00%", "/afiliado/admin/plans/p2/activation", "Novo plano"}},
		{affiliate.ViewAdminAffiliates, affiliate.AdminAffiliatesPageData{Screen: sc, Rows: []affiliate.AffiliateRow{row},
			Table: affiliate.TableState{URL: "/afiliado/admin/affiliates", CurrentPage: 1, Pages: 2, Total: 30}, StoreURL: "/afiliado/admin/affiliates"},
			[]string{"Ana Souza", "/afiliado/admin/affiliates/a1", "Novo afiliado", "/afiliado/admin/commissions"}},
		{affiliate.ViewAdminAffiliate, affiliate.AdminAffiliatePageData{Screen: sc, Affiliate: row, Link: "https://app.example/r/ana",
			Balance: "-R$ 50,00", Held: "R$ 0,00", Available: "R$ 0,00", Negative: true, Clicks: 42, Signups: 7, Sales: 3, Commissions: "R$ 29,85",
			Tree: []affiliate.TreeRow{{Level: 1, User: "user-bruno", Name: "Bruno", Code: "bruno", Status: "Ativo", Joined: "02/10/2026 08:00", URL: "/afiliado/admin/affiliates/a2"},
				{Level: 2, User: "buyer-1", Joined: "03/10/2026 08:00"}},
			BlockURL: "/afiliado/admin/affiliates/a1/block", WriteOffURL: "/afiliado/admin/affiliates/a1/write-off", CommissionsURL: "/afiliado/admin/commissions?affiliate=a1"},
			[]string{"Árvore de indicação", "Bruno (bruno)", "buyer-1", "Perdoar saldo negativo", "/afiliado/admin/affiliates/a1/write-off", "Bloquear"}},
		{affiliate.ViewAdminCommissions, affiliate.AdminCommissionsPageData{Screen: sc, Rows: []affiliate.CommissionRow{commission}, Table: table},
			[]string{"Comissões", "pedido-1", "/afiliado/admin/commissions/c1/reversal", "Estornar"}},
		{affiliate.ViewAdminWithdrawals, affiliate.AdminWithdrawalsPageData{Screen: sc, Rows: []affiliate.WithdrawalRow{withdrawal, paying}, URL: "/afiliado/admin/withdrawals"},
			[]string{"Saques dos afiliados", "/afiliado/admin/withdrawals/w1/processing", "/afiliado/admin/withdrawals/w2/payment", "end_to_end_id"}},
	}
	failed := false
	for _, p := range pages {
		rec := httptest.NewRecorder()
		if err := view.NewRenderer().Render(context.Background(), rec, 200, p.name, p.data); err != nil {
			fmt.Printf("%s: %v\n", p.name, err)
			failed = true
			continue
		}
		body := rec.Body.String()
		for _, want := range p.want {
			if !strings.Contains(body, want) {
				fmt.Printf("%s: the page does not say %q\n", p.name, want)
				failed = true
			}
		}
		for _, key := range []string{"affiliate.screen.", "affiliate.field.", "affiliate.control.", "affiliate.noun."} {
			if strings.Contains(body, key) {
				fmt.Printf("%s: the page draws the catalogue key %s instead of its sentence\n", p.name, key)
				failed = true
			}
		}
	}
	if failed {
		os.Exit(1)
	}
}
`

// inProject runs one command in the scratch project, outside any workspace,
// and fails with what it printed.
//
// -mod=mod lets the go command fill in the requirements the application's
// go.mod leaves to this package's, which is what an application's own `go mod
// tidy` would have written.
func inProject(t *testing.T, dir, failure string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s:\n$ %s %s\n%s", failure, name, strings.Join(args, " "), output)
	}
}
