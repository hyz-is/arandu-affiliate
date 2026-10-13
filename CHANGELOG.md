# Changelog

Everything worth knowing about a release of Arandu Affiliate is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

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

