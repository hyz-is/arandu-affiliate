package affiliate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
)

// The refusals of a referral, each a reason the sign-up keeps going without
// one. They are validation errors: the application decides whether to tell
// the person.
var (
	// ErrSelfReferral is an affiliate signing up with their own code.
	ErrSelfReferral = validation.Errors{"code": {"an affiliate cannot refer themselves"}}
	// ErrSameTree is a document, or a user, already in the referrer's tree.
	ErrSameTree = validation.Errors{"document": {"this person is already in the referrer's tree"}}
	// ErrDocumentReferred is a document another referral already carries.
	ErrDocumentReferred = validation.Errors{"document": {"this document was already referred"}}
)

// maxTreeWalk bounds a walk up the tree. The tree is acyclic by
// construction; the bound is what keeps a corrupted row from looping.
const maxTreeWalk = 64

// ClickRequest is what opening a referral link records.
type ClickRequest struct {
	Code       string
	LandingURL string
	// Address is the visitor's address. Only its hash, with the tenant in it,
	// is stored.
	Address string
}

// RecordClick records that a referral link was opened and answers the
// affiliate it belongs to and how many days the cookie lives. A code that is
// no active affiliate's records nothing and answers no affiliate: the link
// still lands, and sets no cookie.
func (s *AffiliateService) RecordClick(ctx context.Context, actor security.Subject, in ClickRequest) (*Affiliate, int, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ReferralRecord, Affiliate{})
	if err != nil {
		return nil, 0, err
	}
	code := strings.ToLower(strings.TrimSpace(in.Code))
	if !codePattern.MatchString(code) {
		code = ""
	}
	owner, err := Affiliates(s.db).Where("code", "=", code).First(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	if owner == nil || !owner.IsActive() {
		return nil, 0, nil
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ReferralRecord, *owner); err != nil {
		return nil, 0, err
	}
	click, err := Clicks(s.db).New()
	if err != nil {
		return nil, 0, err
	}
	if click.ID, err = data.NewID(); err != nil {
		return nil, 0, err
	}
	now := s.now()
	landing := in.LandingURL
	if len(landing) > maxURLLen {
		landing = landing[:maxURLLen]
	}
	click.TenantID, click.AffiliateID, click.At = data.Tenant(g), owner.ID, now
	click.LandingURL, click.IPHash = landing, addressHash(data.Tenant(g), in.Address)
	click.CreatedAt, click.UpdatedAt = now, now
	if _, err := click.Save(ctx, g); err != nil {
		return nil, 0, err
	}
	plan, err := s.activePlan(ctx, g)
	if err != nil {
		return nil, 0, err
	}
	_, _, days := s.limits(plan)
	return owner, days, nil
}

// addressHash is the visitor's address as it is kept: a hash with the tenant
// in it, so two clicks from one address can be told apart from two clicks
// from two, and the address itself is not stored.
func addressHash(tenant, address string) string {
	if address == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tenant + "\x00" + address))
	return hex.EncodeToString(sum[:16])
}

// AttributeRequest is what a sign-up with a referral code records.
type AttributeRequest struct {
	// UserID is the user who just signed up.
	UserID string
	// Code is the referral code, from the cookie the link set (CodeFrom).
	Code string
	// Document is the user's CPF or CNPJ, when the application has it.
	// Without it, the tree is guarded by the user alone.
	Document string
}

// Attribute links a user who signed up with a referral code to the
// affiliate who owns it, for life. It is called by the application's sign-up,
// with whatever subject it signs up as -- a guest of the tenant included.
//
// A user who already has a referrer keeps them: the answer is the existing
// referral, unchanged. An empty code records nothing and answers nil. A code
// that is no active affiliate's, an affiliate's own code, or a person already
// in the referrer's tree is refused, and the sign-up goes on without a
// referrer.
func (s *AffiliateService) Attribute(ctx context.Context, actor security.Subject, in AttributeRequest) (*Referral, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ReferralRecord, Affiliate{})
	if err != nil {
		return nil, err
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		return nil, validation.Errors{"user_id": {"is required"}}
	}
	if existing, err := Referrals(s.db).Where("user_id", "=", userID).First(ctx, g); err != nil || existing != nil {
		return existing, err
	}
	code := strings.ToLower(strings.TrimSpace(in.Code))
	if code == "" {
		return nil, nil
	}
	var document string
	if strings.TrimSpace(in.Document) != "" {
		if document, err = normalizeDocument(in.Document); err != nil {
			return nil, validation.Errors{"document": {"is not a valid CPF or CNPJ"}}
		}
	}
	referrer, err := Affiliates(s.db).Where("code", "=", code).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if referrer == nil || !referrer.IsActive() {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ReferralRecord, *referrer); err != nil {
		return nil, err
	}
	if referrer.UserID == userID {
		return nil, ErrSelfReferral
	}

	// Up the tree from the referrer: neither the user nor their document may
	// already be in it, or one person would be paid on their own purchases
	// and the tree would close on itself.
	current := referrer
	for depth := 0; current != nil && depth < maxTreeWalk; depth++ {
		if current.UserID == userID || (document != "" && current.Document == document) {
			return nil, ErrSameTree
		}
		above, err := Referrals(s.db).Where("user_id", "=", current.UserID).First(ctx, g)
		if err != nil {
			return nil, err
		}
		if above == nil {
			break
		}
		if document != "" && string(above.Document) == document {
			return nil, ErrSameTree
		}
		if current, err = Affiliates(s.db).WhereKey(above.AffiliateID).First(ctx, g); err != nil {
			return nil, err
		}
	}

	referral, err := Referrals(s.db).New()
	if err != nil {
		return nil, err
	}
	if referral.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	now := s.now()
	referral.TenantID, referral.UserID, referral.AffiliateID = data.Tenant(g), userID, referrer.ID
	referral.Document, referral.Code = OptionalText(document), code
	referral.CreatedAt, referral.UpdatedAt = now, now
	if _, err := referral.Save(ctx, g); err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			// Two sign-ups of one user at once: the one that won is the
			// referral. A document already referred is refused.
			if winner, ferr := Referrals(s.db).Where("user_id", "=", userID).First(ctx, g); ferr == nil && winner != nil {
				return winner, nil
			}
			return nil, ErrDocumentReferred
		}
		return nil, err
	}
	return referral, nil
}

// TreeNode is one person in an affiliate's tree.
type TreeNode struct {
	Level       int
	UserID      string
	ParentID    string
	AffiliateID string
	Name        string
	Code        string
	Status      AffiliateStatus
	JoinedAt    time.Time
}

// Tree is an affiliate's tree, down to MaxLevels: the users they referred,
// and, for those who are affiliates themselves, the users those referred.
func (s *AffiliateService) Tree(ctx context.Context, actor security.Subject, affiliateID string) ([]TreeNode, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AffiliatesManage, Affiliate{})
	if err != nil {
		return nil, err
	}
	root, err := Affiliates(s.db).WhereKey(affiliateID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, ErrNotFound
	}
	var out []TreeNode
	frontier := []*Affiliate{root}
	for level := 1; level <= MaxLevels && len(frontier) > 0; level++ {
		ids := make([]any, 0, len(frontier))
		parents := map[string]*Affiliate{}
		for _, a := range frontier {
			ids = append(ids, a.ID)
			parents[a.ID] = a
		}
		referred, err := Referrals(s.db).WhereIn("affiliate_id", ids).OrderBy("created_at").Get(ctx, g)
		if err != nil {
			return nil, err
		}
		users := make([]any, 0, len(referred))
		for _, r := range referred {
			users = append(users, r.UserID)
		}
		byUser := map[string]*Affiliate{}
		if len(users) > 0 {
			affiliates, err := Affiliates(s.db).WhereIn("user_id", users).Get(ctx, g)
			if err != nil {
				return nil, err
			}
			for _, a := range affiliates {
				byUser[a.UserID] = a
			}
		}
		var next []*Affiliate
		for _, r := range referred {
			node := TreeNode{Level: level, UserID: r.UserID, ParentID: r.AffiliateID, JoinedAt: r.CreatedAt}
			if a := byUser[r.UserID]; a != nil {
				node.AffiliateID, node.Name, node.Code, node.Status = a.ID, a.Name, a.Code, a.Status
				next = append(next, a)
			}
			out = append(out, node)
		}
		frontier = next
	}
	return out, nil
}
