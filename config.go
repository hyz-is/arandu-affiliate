package affiliate

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/arandu-io/hesape/translation"
	wallet "github.com/hyz-is/arandu-wallet"
)

const (
	// DefaultPrefix is where the panels are mounted when Config leaves Prefix
	// empty.
	DefaultPrefix = "/affiliate"
	// DefaultReferralPath is where the referral link answers: /r/{code}.
	DefaultReferralPath = "/r"
	// DefaultCookieName is the cookie the link sets and the sign-up reads.
	DefaultCookieName = "affiliate_ref"
	// DefaultCookieDays is how long the cookie lives when no plan is active.
	DefaultCookieDays = 30
	// DefaultMinWithdrawal is the smallest withdrawal, in centavos, when no
	// plan is active: R$ 100,00.
	DefaultMinWithdrawal = 10000
	// DefaultPageSize is how many rows one page answers with.
	DefaultPageSize = 25
	// MaxPageSize is the most a page may be configured to answer with.
	MaxPageSize = 200
)

// DefaultLocation is the timezone days are counted in when Config leaves
// Location nil.
const DefaultLocation = "America/Sao_Paulo"

// Config is everything the module needs from the application, checked by New
// before the module exists.
type Config struct {
	// Prefix is the path the panels are mounted under. Empty means
	// DefaultPrefix.
	Prefix string

	// ReferralPath is the path the referral link answers under, outside the
	// prefix and outside the sign-in guard: /r/{code}. Empty means
	// DefaultReferralPath.
	ReferralPath string

	// Tenant is the customer a visitor with no session is read as when they
	// open a referral link. Required: the click has to belong to some tenant,
	// and it cannot be one the request names.
	Tenant string

	// LandingPath is where the referral link sends the visitor after setting
	// the cookie, unless the link carries ?to= with a path of the same site.
	// Empty means "/".
	LandingPath string

	// CookieName is the cookie the link sets and CodeFrom reads. Empty means
	// DefaultCookieName.
	CookieName string

	// SecureCookie marks the cookie Secure. Leave it on in production; a
	// development server over plain HTTP turns it off.
	SecureCookie bool

	// PageSize is how many rows one page answers with. Zero means
	// DefaultPageSize; anything above MaxPageSize is refused.
	PageSize int

	// Location is the timezone a day is counted in: the day of a withdrawal,
	// the period of the panel's numbers. Nil means DefaultLocation.
	Location *time.Location

	// Wallet is arandu-wallet's service, the application's one instance
	// (walletModule.Service()). Required: the commissions are deposited in the
	// affiliate's wallet and withdrawn from it, and this package keeps no
	// balance of its own.
	Wallet *wallet.WalletService

	// Translator is the application's own catalogue, asked before the one this
	// package ships, key by key: "affiliate.screen.dashboard_title". Optional.
	Translator *translation.Translator
}

// tenantPattern is what a tenant identifier may be.
var tenantPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// Validate reports the first setting that cannot work.
func (c Config) Validate() error {
	for _, p := range []struct{ name, value string }{{"Prefix", c.Prefix}, {"ReferralPath", c.ReferralPath}, {"LandingPath", c.LandingPath}} {
		if p.value != "" && (p.value[0] != '/' || strings.HasPrefix(p.value, "//")) {
			return fmt.Errorf("affiliate: Config.%s is %q and has to be a path of this site, starting with a single /", p.name, p.value)
		}
	}
	if c.Prefix != "" || c.ReferralPath != "" {
		if err := validateRoutePrefix(c.withDefaults()); err != nil {
			return err
		}
	}
	if c.Tenant == "" {
		return fmt.Errorf("affiliate: Config.Tenant is required: a visitor who opens a referral link has no session, and their click has to belong to some customer that the request does not name")
	}
	if !tenantPattern.MatchString(c.Tenant) {
		return fmt.Errorf("affiliate: Config.Tenant is %q, which cannot be a tenant: lowercase letters, digits, - and _, up to 64 characters", c.Tenant)
	}
	if c.Wallet == nil {
		return fmt.Errorf("affiliate: Config.Wallet is nil: arandu-affiliate needs arandu-wallet installed and registered in bootstrap/app.go; pass walletModule.Service() here")
	}
	if c.PageSize < 0 || c.PageSize > MaxPageSize {
		return fmt.Errorf("affiliate: Config.PageSize is %d, and has to be between 0 and %d, where 0 means %d", c.PageSize, MaxPageSize, DefaultPageSize)
	}
	if c.CookieName != "" && !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(c.CookieName) {
		return fmt.Errorf("affiliate: Config.CookieName is %q; use letters, digits, - and _", c.CookieName)
	}
	return nil
}

// validateRoutePrefix registers the routes' shapes on a scratch mux, which
// panics on a pattern the router would refuse too.
func validateRoutePrefix(c Config) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("affiliate: Config.Prefix %q or Config.ReferralPath %q cannot be registered as a route path", c.Prefix, c.ReferralPath)
		}
	}()
	handler := http.NotFoundHandler()
	mux := http.NewServeMux()
	mux.Handle(http.MethodGet+" "+c.Prefix+"/me", handler)
	mux.Handle(http.MethodGet+" "+c.Prefix+"/admin/affiliates/{id}", handler)
	mux.Handle(http.MethodGet+" "+c.ReferralPath+"/{code}", handler)
	return nil
}

// withDefaults fills what was left empty.
func (c Config) withDefaults() Config {
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix
	}
	if c.ReferralPath == "" {
		c.ReferralPath = DefaultReferralPath
	}
	if c.LandingPath == "" {
		c.LandingPath = "/"
	}
	if c.CookieName == "" {
		c.CookieName = DefaultCookieName
	}
	if c.PageSize == 0 {
		c.PageSize = DefaultPageSize
	}
	if c.Location == nil {
		if loc, err := time.LoadLocation(DefaultLocation); err == nil {
			c.Location = loc
		} else {
			c.Location = time.FixedZone("BRT", -3*60*60)
		}
	}
	return c
}
