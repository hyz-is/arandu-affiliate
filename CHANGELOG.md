# Changelog

Everything worth knowing about a release of Arandu Affiliate is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

## [0.5.0] - 2026-11-24

The program's admin held as an action, and the guards an application puts in
front of the panels, so a platform whose permissions are administered by
arandu-permission can run it. Compatible with 0.4.0: a Subject that holds
`AdminRole` is admin exactly as before, and a `Config` that sets no guard
registers the routes as before. Nothing to migrate.

### Added

- `AdminAction` (`affiliate.admin`): the admin held as an action, the way
  arandu-billing and arandu-investor hold theirs. It opens what `AdminRole`
  opens, and nothing more. It is held in a group of arandu-permission and read
  by `IsAdmin`; the policy still answers the four actions of the program.
- `IsAdmin(security.Subject)`: `AdminRole`, or `AdminAction` among the actions
  the Subject carries. A guest is never an admin, and a Subject that acts as
  somebody else through impersonation is not an admin on the strength of the
  action alone, as in arandu-billing.
- `Config.Middleware` and `Config.AdminMiddleware`: guards run after
  `RequireAuth` on every signed-in route, and after those on the admin routes
  only. They are where an application resolves a Subject's actions
  (arandu-permission) or puts its operator console's guard. The referral link
  stays outside both.

### Changed

- The policy and the commissions list ask `IsAdmin` where they asked
  `HasRole(AdminRole)`. The answer is the same for every Subject that held the
  role.

## [0.4.0] - 2026-11-24

The commercial rules of a plan, for a contract that pays a commission for a
limited time, on part of an annual payment, after a grace period, and not on a
buyer who already paid. Every rule is off at zero, so a plan that sets none pays
as before. Requires `aru migrate`.

### Added

- Plan rules: `MaxMonths` and `MaxPayments` end a buyer's commission, counted
  from their first referred payment; `AnnualBaseBps` is the share of an annual
  payment (`ConversionRequest.Annual`) the rates apply to; `GraceDays` keeps a
  commission pending before it reaches the wallet; `PriorPayerMonths` withholds
  the commission of a buyer who had already paid. A payment the rules leave
  without a commission is recorded, with `Conversion.SkippedReason`
  (`SkipDurationLimit`, `SkipPaymentLimit`, `SkipPriorPayer`), and not rejected.
- `ConversionRequest.Annual`, `BuyerDocument` and `PriorPayer`; `Conversion.Annual`,
  `BuyerDocument` and `SkippedReason`; `Commission.BaseAmount` and `ReleaseAt`.
- `CommissionPending` and `CommissionCancelled`. `ReleaseDue(ctx, actor, now)`
  deposits the commissions whose grace period ended, once each; a refund inside
  the grace period cancels the pending commission without touching the wallet.
  `Balance.Pending` and `Stats.Pending` read what is waiting.
- Coupons: `Affiliate.CouponCode`, `CreateRequest.CouponCode`, `SetCoupon` and
  the route `POST {prefix}/admin/affiliates/{id}/coupon`, named
  `affiliate.admin.affiliates.coupon`. `AttributeRequest.Coupon` prevails over
  `Code` and moves a user who has not paid; `Referral.Source` is `ReferralLink`
  or `ReferralCoupon`. New refusals: `ErrCouponNotFound`, `ErrCouponTooLate`,
  `ErrCouponTaken`.
- The panels: the plan form and list carry the rules, the affiliate's panel
  shows the coupon and the pending commissions apart from the balance, and the
  commission lists show when a pending commission is released. The JSON of
  plans, conversions, commissions and the panel carries the new fields.
- Migrations `20261124_0009_add_plan_commercial_rules`,
  `20261124_0010_add_conversion_rule_columns`, `20261124_0011_add_commission_release`,
  `20261124_0012_add_affiliate_coupon` and `20261124_0013_add_referral_source`;
  all additive, with defaults that keep every existing row's behaviour.

### Changed

- The generated queries are rebuilt with the current model builder
  (`ApplyListing` and `Listing`).

- (Shipped in the v0.3.0 and v0.3.1 tags, which were never given notes.) The lists are drawn with Kyse's server-side data table: the three server-paged
  lists hand it a `State` (`TableState.State`) and a `Paginator`
  (`TableState.Pager`) instead of a query, a sort, the page numbers and the
  chosen values, and the columns a reader switched off survive a search, an
  order and a page through the `hide` parameter. A column that started hidden
  starts shown. Requires Kyse v0.53.0.
- The `table` block of the catalogue names the sentences of the new table, in
  `en` and `pt-BR`; `table.showing`, `table.per_page` and `table.page` are gone.

## [0.2.0] - 2026-10-25

### Changed

- Require Go `1.26.10`, the release that fixes the standard library
  vulnerabilities a module built on the Arandu kernel reaches (CVE-2026-111943
  in crypto/x509 and the net/http HTTP/2 and CONNECT fixes), Framework
  `v0.66.0` and Hesape `v0.85.0`; the manifest floor is
  `framework = ">= 0.66"`.

## [0.1.0] - 2026-10-13

The first release: an affiliate program over arandu-wallet.

### Added

- Affiliates: a login user with a referral code, unique per tenant, and a
  CPF or CNPJ checked by arandu-br; blocked affiliates keep their history and
  earn nothing new.
- The referral link `/r/{code}`, public: it records the click with only a hash
  of the address, sets a cookie for the plan's cookie days (30 by default), and
  lands the visitor on `?to=` when it is a path of the same site. The latest
  link opened replaces the cookie. `Module.CodeFrom` reads it for the sign-up.
- `Attribute`: the lifelong link of a user to the affiliate whose code they
  signed up with. No self-referral, and no person -- by user or by document --
  twice in a tree.
- Commission plans, linear or of up to four levels, with a rate per level, the
  cookie days and the withdrawal limits; one in force at a time, never edited.
- `RecordConversion`: every payment of a referred user, the first purchase and
  every renewal, pays each level up the tree at once into the affiliate's
  `affiliate` wallet, opened with `PayoutVia: "affiliate"`. A blocked level is
  skipped. Idempotent by order reference.
- `RecordRefund` and `ReverseCommission`: the commission leaves the wallet even
  past zero; a negative balance closes withdrawals until it is covered, and
  `WriteOff` forgives it with a recorded reason.
- Withdrawals through arandu-wallet: payout accounts (PIX, bank, crypto,
  other) in the affiliate's own name and document; any amount between the
  plan's minimum and maximum, at most once a day under concurrent requests; the
  operator's queue, payment with receipt, rejection that frees the day.
- Six screens on Kyse components and `components.DataTable`, in pt-BR and
  English.
- The actions `DashboardView`, `PlansManage`, `AffiliatesManage` and
  `ReferralRecord`, and the role `affiliate.admin`.
- The migrations `20261013_0001_create_affiliates`,
  `20261013_0002_create_affiliate_referrals`,
  `20261013_0003_create_affiliate_plans`,
  `20261013_0004_create_affiliate_plan_levels`,
  `20261013_0005_create_affiliate_clicks`,
  `20261013_0006_create_affiliate_conversions`,
  `20261013_0007_create_affiliate_commissions` and
  `20261013_0008_create_affiliate_withdrawal_days`.

