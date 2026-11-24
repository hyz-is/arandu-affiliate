package feature_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

// What an application that administers permissions through arandu-permission
// does: it resolves the actions of the signed-in person onto the request in a
// guard, and the package opens its admin side for the action, with no role.

// resolveAs makes every signed-in request carry the subject given, the way a
// permission guard replaces the one the session loaded.
func resolveAs(who security.Subject) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithSubject(r.Context(), who)))
		})
	}
}

// signedUp starts a session for the id and answers its cookie.
func (w world) signedUp(t *testing.T, id string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if _, err := w.sessions.Start(context.Background(), rec, security.Subject{ID: id, Tenant: tenant}); err != nil {
		t.Fatal(err)
	}
	return rec.Result().Cookies()[0]
}

func send(w world, method, target, body string, c *http.Cookie) *httptest.ResponseRecorder {
	router := fhttp.NewRouter()
	w.module.Routes(router.ForModule(w.module.Name()))
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

const newPlan = "name=Padr%C3%A3o&kind=linear&rates=1%2C5&activate=1"

func TestTheAdminSideOpensForTheActionAMiddlewareResolved(t *testing.T) {
	team := security.Subject{ID: "user-7", Tenant: tenant, Actions: []security.Action{affiliate.AdminAction}, Verified: true}
	var ran []string
	mark := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran = append(ran, name+" "+r.URL.Path)
				next.ServeHTTP(w, r)
			})
		}
	}
	w := installWith(t, func(c *affiliate.Config) {
		c.Middleware = []func(http.Handler) http.Handler{resolveAs(team), mark("all")}
		c.AdminMiddleware = []func(http.Handler) http.Handler{mark("admin")}
	})
	cookie := w.signedUp(t, "user-7")

	// Defining a plan is the admin's act, and answers a redirect, not a refusal.
	rec := send(w, http.MethodPost, "/affiliate/admin/plans", newPlan, cookie)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("the admin held as an action was answered %d defining a plan", rec.Code)
	}
	joined := strings.Join(ran, "|")
	if !strings.Contains(joined, "all /affiliate/admin/plans") || !strings.Contains(joined, "admin /affiliate/admin/plans") {
		t.Errorf("the guards did not run on the admin routes: %s", joined)
	}

	ran = nil
	_ = send(w, http.MethodPost, "/affiliate/me/payout-accounts", "kind=pix&pix_key_type=email&pix_key=ana%40example.com", cookie)
	if joined := strings.Join(ran, "|"); !strings.Contains(joined, "all /affiliate/me") || strings.Contains(joined, "admin ") {
		t.Errorf("Middleware must run on the panel and AdminMiddleware must not: %s", joined)
	}
}

func TestAPersonWhoseActionsDoNotIncludeTheAdminStaysOutEvenWithAMiddleware(t *testing.T) {
	someone := security.Subject{ID: "user-8", Tenant: tenant, Actions: []security.Action{"billing.admin"}, Verified: true}
	w := installWith(t, func(c *affiliate.Config) {
		c.Middleware = []func(http.Handler) http.Handler{resolveAs(someone)}
	})
	rec := send(w, http.MethodPost, "/affiliate/admin/plans", newPlan, w.signedUp(t, "user-8"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("another package's admin was answered %d defining a plan, want 403", rec.Code)
	}
}

func TestWithNoMiddlewareAPersonWithoutTheRoleStaysOutOfTheAdminSide(t *testing.T) {
	w := install(t)
	cookie := w.signedUp(t, "user-7")
	rec := send(w, http.MethodPost, "/affiliate/admin/plans", newPlan, cookie)
	if rec.Code != http.StatusForbidden {
		t.Errorf("somebody with no role and no action was answered %d defining a plan, want 403", rec.Code)
	}
}

func TestTheAdminHeldAsAnActionSeesEveryCommission(t *testing.T) {
	w := install(t)
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	planOf(t, w, affiliate.Linear, 10000, 0, 150)
	signUp(t, w, "user-bia", "ana", cpfBruno)
	pay(t, w, "order-1", "user-bia", 10000, false)

	team := security.Subject{ID: "user-7", Tenant: tenant, Actions: []security.Action{affiliate.AdminAction}, Verified: true}
	rows, total, err := w.svc.CommissionsPage(context.Background(), team, affiliate.CommissionsQuery{AffiliateID: ana.ID})
	if err != nil || total == 0 || len(rows) == 0 {
		t.Fatalf("the admin held as an action reads %d commissions: %v", total, err)
	}
}
