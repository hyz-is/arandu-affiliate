package feature_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/framework/security"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

// The coupon of an affiliate: unique in the tenant, typed by the customer at
// checkout, and valid only until the first payment.

func TestACouponTypedAtCheckoutPrevailsOverTheLink(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	bruno := enroll(t, w, "user-bruno", "bruno", cpfBruno)
	rulesPlan(t, w, affiliate.PlanRequest{})
	if _, err := w.svc.SetCoupon(ctx, admin(), bruno.ID, " BRUNO10 "); err != nil {
		t.Fatalf("giving Bruno a coupon: %v", err)
	}

	// Both arrive at a sign-up: the coupon wins, in any case.
	r, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-1", Code: "ana", Coupon: "Bruno10"})
	if err != nil || r == nil || r.AffiliateID != bruno.ID || r.Source != affiliate.ReferralCoupon {
		t.Fatalf("the sign-up with both was referred to %+v, %v; want Bruno by coupon", r, err)
	}

	// Somebody who came through Ana's link and has not paid is moved by the coupon.
	signUp(t, w, "buyer-2", "ana", "")
	r, err = w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-2", Coupon: "bruno10"})
	if err != nil || r == nil || r.AffiliateID != bruno.ID || r.Source != affiliate.ReferralCoupon || r.Code != "bruno10" {
		t.Fatalf("the coupon left buyer-2 with %+v, %v; want Bruno", r, err)
	}
	if _, paid := payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-2", BuyerUserID: "buyer-2", Amount: monthly, OccurredAt: at(2026, 1, 10)}); len(paid) != 1 || paid[0].AffiliateID != bruno.ID {
		t.Fatalf("the payment paid %+v; want Bruno", paid)
	}

	// Somebody nobody referred, and who has not paid, is referred by it.
	r, err = w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-3", Coupon: "bruno10"})
	if err != nil || r == nil || r.AffiliateID != bruno.ID {
		t.Fatalf("buyer-3 was referred to %+v, %v; want Bruno", r, err)
	}

	// The same coupon again, or a coupon of the current referrer, changes nothing.
	if again, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-3", Coupon: "bruno10"}); err != nil || again.ID != r.ID {
		t.Fatalf("the same coupon twice answered %+v, %v", again, err)
	}

	// Once a payment is on record, even by a buyer nobody referred, the coupon
	// is too late, and the existing referrer stays.
	signUp(t, w, "buyer-4", "ana", "")
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-4", BuyerUserID: "buyer-4", Amount: monthly, OccurredAt: at(2026, 1, 10)})
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-4", Coupon: "bruno10"}); !errors.Is(err, affiliate.ErrCouponTooLate) && !sameErrors(err, affiliate.ErrCouponTooLate) {
		t.Fatalf("a coupon after the first payment answered %v, want ErrCouponTooLate", err)
	}
	kept, _ := w.svc.Upline(ctx, admin(), "buyer-4")
	if len(kept) != 1 || kept[0].ID != ana.ID {
		t.Fatalf("buyer-4's upline is %+v; want Ana, for life", kept)
	}
	payAs(t, w, affiliate.ConversionRequest{OrderRef: "o-5", BuyerUserID: "buyer-5", Amount: monthly, OccurredAt: at(2026, 1, 10)})
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-5", Coupon: "bruno10"}); !sameErrors(err, affiliate.ErrCouponTooLate) {
		t.Fatalf("a coupon from a buyer who paid unreferred answered %v, want ErrCouponTooLate", err)
	}
}

func TestACouponThatBelongsToNobodyIsRefusedAndRecordsNothing(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	enroll(t, w, "user-ana", "ana", cpfAna)
	bruno := enroll(t, w, "user-bruno", "bruno", cpfBruno)
	if _, err := w.svc.SetCoupon(ctx, admin(), bruno.ID, "bruno10"); err != nil {
		t.Fatal(err)
	}

	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-1", Code: "ana", Coupon: "nope"}); !sameErrors(err, affiliate.ErrCouponNotFound) {
		t.Fatalf("an unknown coupon answered %v, want ErrCouponNotFound", err)
	}
	if up, _ := w.svc.Upline(ctx, admin(), "buyer-1"); len(up) != 0 {
		t.Fatalf("the refused coupon left an upline %+v", up)
	}
	// The application may carry on with the cookie's code alone.
	if r := signUp(t, w, "buyer-1", "ana", ""); r == nil || r.Source != affiliate.ReferralLink {
		t.Fatalf("the cookie's code alone referred %+v", r)
	}

	// A blocked affiliate's coupon is no coupon.
	if _, err := w.svc.SetBlocked(ctx, admin(), bruno.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "buyer-2", Coupon: "bruno10"}); !sameErrors(err, affiliate.ErrCouponNotFound) {
		t.Fatalf("a blocked affiliate's coupon answered %v", err)
	}
	// Nobody refers themselves with their own coupon.
	if _, err := w.svc.SetBlocked(ctx, admin(), bruno.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{UserID: "user-bruno", Coupon: "bruno10"}); !sameErrors(err, affiliate.ErrSelfReferral) {
		t.Fatalf("an affiliate's own coupon answered %v, want ErrSelfReferral", err)
	}
}

func TestACouponIsUniqueInTheTenantAndAdminOnly(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := enroll(t, w, "user-ana", "ana", cpfAna)
	bruno := enroll(t, w, "user-bruno", "bruno", cpfBruno)

	if a, err := w.svc.SetCoupon(ctx, admin(), ana.ID, "Promo-10"); err != nil || a.CouponCode != "promo-10" {
		t.Fatalf("giving a coupon answered %+v, %v", a, err)
	}
	if _, err := w.svc.SetCoupon(ctx, admin(), bruno.ID, "PROMO-10"); !sameErrors(err, affiliate.ErrCouponTaken) {
		t.Fatalf("a second affiliate took the coupon: %v", err)
	}
	if _, err := w.svc.SetCoupon(ctx, admin(), bruno.ID, "a"); err == nil {
		t.Fatal("a coupon of one letter was accepted")
	}
	// Two affiliates without a coupon are not the same coupon, and an affiliate
	// created with one keeps it.
	if a, err := w.svc.Create(ctx, admin(), affiliate.CreateRequest{UserID: "user-caio", Name: "Caio", Document: cpfCaio, CouponCode: "CAIO5"}); err != nil || a.CouponCode != "caio5" {
		t.Fatalf("creating with a coupon answered %+v, %v", a, err)
	}
	if _, err := w.svc.SetCoupon(ctx, user("user-ana"), ana.ID, "mine"); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("an affiliate set their own coupon: %v", err)
	}
	// Removing it frees it.
	if a, err := w.svc.SetCoupon(ctx, admin(), ana.ID, ""); err != nil || a.CouponCode != "" {
		t.Fatalf("removing the coupon answered %+v, %v", a, err)
	}
	if _, err := w.svc.SetCoupon(ctx, admin(), bruno.ID, "promo-10"); err != nil {
		t.Fatalf("taking the freed coupon: %v", err)
	}
}
