package affiliate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	wallet "github.com/hyz-is/arandu-wallet"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// AffiliateService is the program's use cases. It owns the database handle;
// every exported method asks the policy before it reads or writes, and every
// statement carries the tenant of the Grant the policy issued.
type AffiliateService struct {
	db     *data.DB
	policy AffiliatePolicy

	// now is the clock, replaceable by a test.
	now func() time.Time

	// loc is the timezone days are counted in.
	loc *time.Location

	// wallets is arandu-wallet, where the commissions are kept.
	wallets *wallet.WalletService

	// defaults are the cookie and the withdrawal limits used while no plan is
	// active.
	cookieDays    int
	minWithdrawal int64
}

// NewAffiliateService returns the use cases over a database handle. New wires
// the rest; a test may build one directly.
func NewAffiliateService(db *data.DB) *AffiliateService {
	return &AffiliateService{
		db: db, now: func() time.Time { return time.Now().UTC() },
		cookieDays: DefaultCookieDays, minWithdrawal: DefaultMinWithdrawal,
	}
}

func (s *AffiliateService) location() *time.Location {
	if s.loc == nil {
		return time.UTC
	}
	return s.loc
}

// codePattern is what a referral code may be: lowercase letters, digits and
// hyphens, three to 32, starting with a letter or a digit.
var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,31}$`)

// codeAlphabet is what a generated code is drawn from: no 0/o, 1/l/i, so a
// code read aloud or copied by hand is the code.
const codeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// newCode draws a referral code of eight characters.
func newCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(buf), nil
}

// CreateRequest is what registering an affiliate takes.
type CreateRequest struct {
	// UserID is the login user who will sign in to the panel.
	UserID string
	// Name is how the affiliate is called.
	Name string
	// Document is the CPF or the CNPJ.
	Document string
	// Code is the referral code. Empty draws one.
	Code string
}

// Validate reports the errors per field.
func (r CreateRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "user_id", r.UserID)
	validation.MaxLen(e, "user_id", r.UserID, maxIdentifierLen)
	validation.Required(e, "name", r.Name)
	validation.MaxLen(e, "name", r.Name, maxNameLen)
	if _, err := normalizeDocument(r.Document); err != nil {
		e.Add("document", "is not a valid CPF or CNPJ")
	}
	if code := strings.ToLower(strings.TrimSpace(r.Code)); code != "" && !codePattern.MatchString(code) {
		e.Add("code", "has to be 3 to 32 lowercase letters, digits or hyphens")
	}
	return e
}

var _ validation.Validatable = CreateRequest{}

// ErrTaken is a user, a document or a code that already belongs to an
// affiliate of the tenant.
var ErrTaken = validation.Errors{"code": {"the user, the document or the code is already an affiliate's"}}

// Create registers an affiliate.
func (s *AffiliateService) Create(ctx context.Context, actor security.Subject, in CreateRequest) (*Affiliate, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	document, _ := normalizeDocument(in.Document)
	code := strings.ToLower(strings.TrimSpace(in.Code))
	if code == "" {
		generated, err := newCode()
		if err != nil {
			return nil, err
		}
		code = generated
	}
	proposed := Affiliate{
		UserID: strings.TrimSpace(in.UserID), Name: strings.TrimSpace(in.Name),
		Document: document, Code: code, Status: AffiliateActive,
	}
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, proposed)
	if err != nil {
		return nil, err
	}
	candidate, err := Affiliates(s.db).New()
	if err != nil {
		return nil, err
	}
	if candidate.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	candidate.TenantID = data.Tenant(g)
	candidate.UserID, candidate.Name = proposed.UserID, proposed.Name
	candidate.Document, candidate.Code, candidate.Status = proposed.Document, proposed.Code, proposed.Status
	candidate.CreatedBy, candidate.CreatedAt, candidate.UpdatedAt = actor.ID, now, now
	if _, err := candidate.Save(ctx, g); err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			return nil, ErrTaken
		}
		return nil, err
	}
	return candidate, nil
}

// SetBlocked blocks an affiliate, or unblocks one. A blocked affiliate earns
// nothing new and cannot withdraw; their history and their place in the tree
// stay.
func (s *AffiliateService) SetBlocked(ctx context.Context, actor security.Subject, id string, blocked bool) (*Affiliate, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	record, err := Affiliates(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, *record); err != nil {
		return nil, err
	}
	record.Status = AffiliateActive
	if blocked {
		record.Status = AffiliateBlocked
	}
	record.UpdatedAt = s.now()
	if _, err := record.Save(ctx, g); err != nil {
		return nil, err
	}
	return record, nil
}

// Find answers one affiliate, for the admin.
func (s *AffiliateService) Find(ctx context.Context, actor security.Subject, id string) (*Affiliate, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	record, err := Affiliates(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, *record); err != nil {
		return nil, err
	}
	return record, nil
}

// Mine answers the affiliate whose login user is asking.
func (s *AffiliateService) Mine(ctx context.Context, actor security.Subject) (*Affiliate, error) {
	g, err := security.Authorize(ctx, s.policy, actor, DashboardView, Affiliate{})
	if err != nil {
		return nil, err
	}
	record, err := Affiliates(s.db).Where("user_id", "=", actor.ID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, DashboardView, *record); err != nil {
		return nil, err
	}
	return record, nil
}

// AffiliatesQuery narrows the admin's list. Page counts from one.
type AffiliatesQuery struct {
	Search  string
	Status  AffiliateStatus
	Sort    string
	Dir     string
	Page    int
	PerPage int
}

// sortableAffiliate is the ordering allowlist of the list.
var sortableAffiliate = map[string]string{
	"": "created_at", "created_at": "created_at", "name": "name", "code": "code", "status": "status",
}

// AffiliatesPage is a page of the admin's list and how many rows it has.
func (s *AffiliateService) AffiliatesPage(ctx context.Context, actor security.Subject, q AffiliatesQuery) ([]*Affiliate, int, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, 0, err
	}
	column, ok := sortableAffiliate[q.Sort]
	if !ok {
		return nil, 0, validation.Errors{"sort": {"is not a column the list can be ordered by"}}
	}
	if q.PerPage <= 0 || q.PerPage > maxLimit {
		q.PerPage = defaultLimit
	}
	if q.Page < 1 {
		q.Page = 1
	}
	filtered := func() *AffiliateQuery {
		b := Affiliates(s.db)
		if q.Status != "" {
			b = b.Where("status", "=", string(q.Status))
		}
		if term := strings.TrimSpace(q.Search); term != "" {
			like := "%" + term + "%"
			b = b.Where(func(w *AffiliateQuery) {
				w.Where("name", "like", like).OrWhere("code", "like", like).OrWhere("user_id", "like", like).OrWhere("document", "like", like)
			})
		}
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	page := filtered()
	if q.Dir == "asc" {
		page = page.OrderBy(column)
	} else {
		page = page.OrderByDesc(column)
	}
	rows, err := page.OrderByDesc("id").Offset((q.Page-1)*q.PerPage).Limit(q.PerPage).Get(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	return rows, int(total), nil
}

// fmtErr is a refusal the router answers with 403, with the reason in the
// log and not in the response.
func forbidden(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, security.ErrForbidden)...)
}

// Upline is who earns on a user's payments: the user's referrer first, then
// the referrer's, up to MaxLevels. Empty for a user nobody referred.
func (s *AffiliateService) Upline(ctx context.Context, actor security.Subject, userID string) ([]*Affiliate, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	var out []*Affiliate
	above, err := Referrals(s.db).Where("user_id", "=", userID).First(ctx, g)
	for level := 1; err == nil && above != nil && level <= MaxLevels; level++ {
		var a *Affiliate
		if a, err = Affiliates(s.db).WhereKey(above.AffiliateID).First(ctx, g); err != nil || a == nil {
			break
		}
		out = append(out, a)
		above, err = Referrals(s.db).Where("user_id", "=", a.UserID).First(ctx, g)
	}
	return out, err
}
