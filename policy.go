package affiliate

import (
	"context"
	"fmt"

	"github.com/arandu-io/framework/security"
)

// The actions of the affiliate program. Constants rather than strings at the
// call site: a typo in an action name would silently authorize nothing, or
// worse, everything.
const (
	// DashboardView is an affiliate reading their own panel: the link, the
	// plan, the numbers, the commissions, the balance and the withdrawals --
	// and asking for a withdrawal or declaring a payout account of their own.
	DashboardView security.Action = "affiliate.dashboard.view"
	// PlansManage is defining the commission plans.
	PlansManage security.Action = "affiliate.plans.manage"
	// AffiliatesManage is the admin's side of the program: the affiliates, the
	// tree, blocking, the commissions and their reversal, the write-off of a
	// negative balance, the queue of withdrawals, and recording the payments
	// the commissions are paid on.
	AffiliatesManage security.Action = "affiliate.affiliates.manage"
	// ReferralRecord is recording what a visitor did with a referral link: the
	// click on /r/{code}, and the referral of the user who signed up with it.
	// The visitor has no session; the application's sign-up is the caller.
	ReferralRecord security.Action = "affiliate.referrals.record"
	// AdminAction is being this package's admin, held as an action: what an
	// application whose permissions arandu-permission administers puts in a
	// group. It opens what AdminRole opens, and nothing more. It is held, not
	// asked: the policy answers the four actions above, and IsAdmin reads this
	// one among the actions a Subject carries.
	AdminAction security.Action = "affiliate.admin"
)

// AdminRole is the role that runs the program in its tenant. The application
// gives it through arandu-permission, which fills the Subject's roles.
const AdminRole = "affiliate.admin"

// IsAdmin says whether the subject runs the program: it holds AdminRole, or
// AdminAction among the actions arandu-permission resolved for it. A guest is
// never an admin, and neither is a subject that acts as somebody else through
// impersonation on the strength of an action alone: an admin acting inside
// somebody's account acts as that account, as arandu-billing decides it.
func IsAdmin(s security.Subject) bool {
	if s.IsGuest() {
		return false
	}
	return s.HasRole(AdminRole) || (!s.Impersonated() && s.Can(AdminAction))
}

// AffiliatePolicy is the only authority over who does what in the program.
//
// It denies unless a rule below allows. The admin role manages the program in
// its own tenant; an affiliate reads only their own row; a visitor -- with or
// without a session -- may record a click and a referral and nothing else.
type AffiliatePolicy struct{}

// Compile-time proof that the policy answers about this entity and no other.
var _ security.Policy[Affiliate] = AffiliatePolicy{}

// Can decides whether the subject may perform the action on the record.
//
// It is the only place that decides. The service reaches the Model only after
// Authorize turns this method's nil result into a Grant.
func (AffiliatePolicy) Can(ctx context.Context, s security.Subject, a security.Action, record Affiliate) error {
	// Tenant isolation comes first and applies to every action.
	//
	// The empty id is the candidate that has not been stored yet, or the probe
	// a listing asks before it reads, which belongs to nobody.
	if record.ID != "" && record.TenantID != s.Tenant {
		return fmt.Errorf("affiliate belongs to another tenant")
	}

	// arandu:begin custom

	// A referral link is opened by anybody, and a sign-up is somebody who has
	// no session yet: recording either is allowed to every subject of the
	// tenant, guests included, and to nothing else.
	if a == ReferralRecord {
		return nil
	}

	if s.IsGuest() {
		return fmt.Errorf("a guest is no affiliate")
	}

	if IsAdmin(s) {
		switch a {
		case PlansManage, AffiliatesManage, DashboardView:
			return nil
		}
	}

	// An affiliate reads their own panel. The probe -- no record yet -- is
	// answered yes, and the service asks again about the row it loaded.
	if a == DashboardView {
		if record.ID == "" {
			return nil
		}
		if record.UserID != "" && record.UserID == s.ID {
			return nil
		}
	}

	// arandu:end custom

	return fmt.Errorf("no rule allows %s on affiliate", a)
}
