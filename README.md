<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Arandu Affiliate

An affiliate program for an Arandu application: a referral link per
affiliate, a referral tree that is for life, commission plans of one to four
levels, commissions paid into an [arandu-wallet](https://github.com/hyz-is/arandu-wallet)
wallet the moment a referred user pays, and withdrawals an operator pays by
PIX with the receipt attached.

## How it works

1. **The link.** Every affiliate has a code, and `/r/{code}` records the
   click, sets a cookie that carries the code for the plan's cookie days (30 by
   default), and lands the visitor — on `?to=` when it is a path of the same
   site. A link opened later replaces the cookie.
2. **The sign-up.** The application's sign-up reads the cookie
   (`module.CodeFrom(r)`) and calls `Attribute`. The user is then linked to that
   affiliate **for life**: no later cookie moves them. An affiliate cannot refer
   themselves, and one person — by user or by CPF/CNPJ — cannot appear twice in a
   tree.
3. **The payment.** The application reports every payment of a user —
   the first purchase and every renewal — with `RecordConversion`. The package
   walks up the tree from the buyer and pays each level the rate of the plan in
   force: level one is the buyer's referrer, level two the referrer's referrer,
   up to four. A blocked affiliate's level is skipped; the levels above still
   earn. The commission is deposited at once, unless the plan sets a grace
   period (see [Commercial rules](#commercial-rules)), and stays in the wallet
   until withdrawn.
4. **The refund.** `RecordRefund` reverses every commission of the payment.
   The money leaves the wallet even past zero: a commission already withdrawn
   leaves the balance negative, which closes withdrawals until later
   commissions cover it, or the admin forgives the rest with a recorded
   write-off.
5. **The withdrawal.** The affiliate declares a payout account — PIX, bank,
   crypto or another kind — always in the name and document of their
   registration, and asks for any amount between the plan's minimum and
   maximum (R$ 100,00 minimum by default), at most once a day. arandu-wallet
   holds the amount; the operator takes the request, pays it outside the
   system and attaches the end-to-end identifier and the receipt, or rejects it
   with a reason, which frees the day.

Money is `int64` centavos and rates are basis points (150 = 1,5%).

## Commercial rules

A plan can carry the rules of a commercial contract. Every one is **off at
zero**, so a plan that sets none pays exactly as it always did, and the plans
written before the rules existed keep doing so after `aru migrate`.

| `PlanRequest` field | what it does | zero means |
| --- | --- | --- |
| `MaxMonths` | a payment made that many calendar months after the buyer's first referred payment pays nothing. Months are calendar months, clamped to the end of a shorter month (August 31 plus six months is February 28) | no limit |
| `MaxPayments` | a payment after that many commissioned payments of the buyer pays nothing. With `MaxMonths`, whichever comes first | no limit |
| `AnnualBaseBps` | the share of an annual payment (`ConversionRequest.Annual`) the rates apply to; 5000 pays on half | the whole payment (same as `FullBaseBps`, 10000) |
| `GraceDays` | days after the payment (`OccurredAt`) a commission stays pending, outside the wallet, until `ReleaseDue` deposits it | deposited at once |
| `PriorPayerMonths` | a buyer who had already paid within that many months before their first referred payment earns the affiliate nothing, for life | rule off |

A payment past a limit, or by a prior payer, is **recorded and pays nothing**,
not rejected: the conversion keeps its place in the history, is idempotent by
`OrderRef` like any other, and says why in `Conversion.SkippedReason`
(`duration_limit`, `payment_limit` or `prior_payer`). A refund of it is a no-op
on commissions. The rules read the plan in force when each payment arrives.

**The clock starts at the buyer's first referred payment**: the earliest counted
conversion of that user that carried a referrer. A free trial is no payment — the
application simply does not call `RecordConversion` until money moves.

**Annual payments.** `ConversionRequest.Annual` marks a payment that covers a long
period. The rate applies to `roundDiv(amount * AnnualBaseBps, 10000)`, half up to
the centavo, and then `CommissionOf` rounds the commission half up the same way;
`Commission.BaseAmount` records the base. An annual commission is one deposit.
Its renewals respect `MaxMonths` and `MaxPayments` like any other payment.
For the contract's numbers — 30% over six months, annual at half:

| payment | base | commission |
| --- | --- | --- |
| monthly R$ 24,90, six payments | 24,90 each | 7,47 each, **R$ 44,82** |
| annual R$ 238,80 | 119,40 | **R$ 35,82** |

**Grace and release.** Under `GraceDays` a commission is `pending`: the affiliate's
panel shows it apart from the balance, and it cannot be withdrawn. The
application calls `ReleaseDue` from a daily job:

```go
	released, err := affiliateModule.Service().ReleaseDue(ctx, operator, time.Now())
```

It deposits each pending commission whose release date has come, with the same
wallet key `RecordConversion` uses for an immediate one, so running it twice or
twice at once deposits once. The wallet receives money only at release. A refund
inside the grace period (`RecordRefund`, or the admin's reversal) **cancels** the
pending commission (`cancelled`) and never touches the wallet; a refund after the
release keeps the old behaviour — the amount leaves the wallet, the balance may go
below zero, and later commissions cover it. A payment reported after its own grace
period has already passed is deposited at once.

**The buyer who already paid.** With `PriorPayerMonths` above zero, the module looks
in its own conversions for a counted payment of the same `BuyerUserID`, or of the
same `BuyerDocument`, in the window before the first referred payment. What the
module cannot see, the application tells it with `ConversionRequest.PriorPayer`,
read only while the plan asks for the rule. A buyer found to be a prior payer stays
one on every later payment.

**Coupons.** An affiliate may have one `CouponCode`, unique in the tenant and stored
lowercase (`SetCoupon`, or `CreateRequest.CouponCode`; admin only). The customer's
own typing at checkout carries it to `Attribute` as `AttributeRequest.Coupon`:

- the coupon **prevails over the cookie's code** when both come;
- it refers a user nobody referred, and **moves** a user who came through another
  affiliate's link, **only while that user has no payment on record** (a refunded
  one counts). After the first payment the request is refused with
  `ErrCouponTooLate` and the referrer stays for life;
- the module cannot tell who typed a coupon, so the application passes only what
  the customer typed at checkout, never one an affiliate asked for afterwards;
  no call moves a user who already has a payment on record;
- a coupon that belongs to no active affiliate is refused with `ErrCouponNotFound`
  and nothing is recorded, so the checkout may carry on with the cookie's code
  alone. An affiliate's own coupon is `ErrSelfReferral`.

Whoever has no coupon is unaffected, and a referral made by link stays for life.

## Install

It needs arandu-wallet v0.13.0 or newer, whose wallets take withdrawal
requests only through the package that manages them:

```bash
go get github.com/hyz-is/arandu-wallet@v0.13.0
go get github.com/hyz-is/arandu-affiliate
```

## Wire it

An Arandu application registers a module explicitly, in `bootstrap/app.go`.

The import, with the other module imports:

```go
import (
	affiliate "github.com/hyz-is/arandu-affiliate"
)
```

The construction, in `App`, once `NewAppServiceProvider` has returned and the
wallet module exists. `New` refuses a wiring that cannot work and `Configure`
has no way to return that error, so the module is built where the error can
leave; the database and the session store are the ones the framework built from
the configuration:

```go
	affiliateModule, err := affiliate.New(affiliate.Config{
		Tenant:       "acme",                   // the customer a visitor with no session is read as
		Wallet:       walletModule.Service(),   // the one arandu-wallet instance
		SecureCookie: cfg.App.Env == "production",
	}, drivers.DB, drivers.Sessions)
	if err != nil {
		_ = drivers.Close()
		return nil, err
	}
```

And the registration, in the `WithProviders(...)` chain the builder is made of:

```go
	return Configure(cfg, drivers, app).
		WithProviders(affiliateModule).
		WithVersion(version, commit).
		Create()
```

Then, once:

```bash
aru migrate
```

The package owns eight tables; `arandu.mod.toml` says `migrations = true`.

### In the sign-up

```go
	referral, err := affiliateModule.Service().Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{
		UserID:   newUser.ID,
		Code:     affiliateModule.CodeFrom(r),
		Document: newUser.Document, // optional; guards the tree by CPF/CNPJ too
	})
```

At checkout, the coupon the customer typed goes in `Coupon`; it prevails over `Code`
until their first payment:

```go
	referral, err := affiliateModule.Service().Attribute(ctx, security.Guest(tenant), affiliate.AttributeRequest{
		UserID: payer.ID, Coupon: typedCoupon, Code: affiliateModule.CodeFrom(r),
	})
```

A refusal is a `validation.Errors` the sign-up may ignore: the user is created
either way, without a referrer.

### Where payments are confirmed

```go
	_, _, err := affiliateModule.Service().RecordConversion(ctx, operator, affiliate.ConversionRequest{
		OrderRef: "mp:" + payment.ID, BuyerUserID: payment.UserID, ProductID: "placaflow",
		Amount: 19900, Renewal: payment.IsRenewal, OccurredAt: payment.ApprovedAt,
		Annual: payment.IsAnnual, BuyerDocument: payer.Document, // read by the plan's rules
	})
	// and when it is given back:
	_, err = affiliateModule.Service().RecordRefund(ctx, operator, "mp:"+payment.ID, "chargeback")
```

Both are idempotent by the order reference. `operator` is a subject with the
roles below.

## Give people access

Roles reach the policy through the Subject, which
[arandu-permission](https://github.com/hyz-is/arandu-permission) fills:

| role | what it opens |
| --- | --- |
| `affiliate.admin` | plans, affiliates, the tree, blocking, commissions and their reversal, write-offs, the withdrawal queue, recording payments |
| `wallet.operator` | arandu-wallet's operator actions; whoever records payments or pays withdrawals needs it too |

An affiliate needs no role: the user whose `user_id` is on an affiliate's
registration reads that panel and nobody else's. A visitor may only record a
click and a referral. The tenant of every statement is the Grant's.

## Screens

Six, in pt-BR and English, on Kyse components and `components.DataTable`: the
affiliate's panel — link with a copy button, the plan in force, the period's
clicks, sign-ups, sales and commissions, the balance, the withdrawal form, the
payout account, the commissions filtered by level and status and paged on the
server, the withdrawals with their receipts — and the admin's plans,
affiliates (paged, searched, filtered on the server), one affiliate with the
tree and the write-off, the commissions with manual reversal, and the
withdrawal queue. Every `GET` answers the screen to a browser and JSON to a
client that asks for it.

## Publish the views

```bash
aru vendor:publish --tag=view --apply
aru view:build
```

and import the two directories `aru view:build` writes:

```go
	_ "your/module/path/storage/framework/views/modules/affiliate"
	_ "your/module/path/storage/framework/views/modules/affiliate/admin"
```

The module refuses to boot until they are linked. The sentences come from
`resources/lang/<locale>/affiliate.json`; the application's own catalogue,
passed as `Config.Translator`, is asked first, key by key.

## Configuration

| field | required | meaning |
| --- | --- | --- |
| `Tenant` | yes | the customer a visitor with no session is read as on `/r/{code}`. |
| `Wallet` | yes | arandu-wallet's service. The package keeps no balance of its own. |
| `Prefix` | no | where the panels are mounted. Defaults to `/affiliate`. |
| `ReferralPath` | no | where the link answers. Defaults to `/r`. |
| `LandingPath` | no | where the link lands without `?to=`. Defaults to `/`. |
| `CookieName` | no | the referral cookie. Defaults to `affiliate_ref`. |
| `SecureCookie` | no | marks the cookie Secure; on in production. |
| `PageSize` | no | rows per page. Defaults to 25, refused above 200. |
| `Location` | no | the timezone days are counted in. Defaults to `America/Sao_Paulo`. |
| `Translator` | no | the application's catalogue, asked first. |

## Routes

`GET /r/{code}` is public. Every other route sits behind `RequireAuth`: a
visitor with no session is sent to `/auth/login`, and a handler reads who is
asking with `ctx.User()`.

| method | path | what |
| --- | --- | --- |
| `GET` | `/r/{code}` | the referral link |
| `GET` | `/affiliate/me` | the affiliate's panel |
| `POST` | `/affiliate/me/payout-accounts` | declare a payout account |
| `POST` | `/affiliate/me/withdrawals` | ask for a withdrawal |
| `GET` | `/affiliate/withdrawals/{id}/receipt` | the receipt of a paid withdrawal |
| `GET`, `POST` | `/affiliate/admin/plans` | the plans, define one |
| `POST` | `/affiliate/admin/plans/{id}/activation` | make a plan the one in force |
| `GET`, `POST` | `/affiliate/admin/affiliates` | the affiliates, register one |
| `GET` | `/affiliate/admin/affiliates/{id}` | one affiliate, with the tree |
| `POST`, `DELETE` | `/affiliate/admin/affiliates/{id}/block` | block, unblock |
| `POST` | `/affiliate/admin/affiliates/{id}/coupon` | give, change or remove the coupon |
| `POST` | `/affiliate/admin/affiliates/{id}/write-off` | forgive a negative balance |
| `GET` | `/affiliate/admin/commissions` | every commission |
| `POST` | `/affiliate/admin/commissions/{id}/reversal` | reverse a commission |
| `POST` | `/affiliate/admin/conversions` | record a payment |
| `POST` | `/affiliate/admin/refunds` | record a refund |
| `GET` | `/affiliate/admin/withdrawals` | the withdrawal queue |
| `POST` | `/affiliate/admin/withdrawals/{id}/processing`, `/payment`, `/rejection` | take, pay (multipart, with the receipt), reject |

## The data path

Each entity embeds `model.Model` and declares its table beside it.
`aru model:build` generates the queries — `AffiliateQuery.go` with
`Affiliates(db)`, and one per entity — committed and never edited by hand;
`aru model:build --check` fails when one is out of date.

`AffiliateService` owns `*data.DB` and follows
`validate -> security.Authorize -> Grant -> Model terminal` in every exported
method, which the unit suite audits. Every query reads the tenant from the
Grant through `data.Tenant(g)`.

## Example

```bash
go run -tags example ./example
```

It opens SQLite in a temporary directory, migrates arandu-wallet and this
package, and runs the program: a two-level plan, two affiliates in a tree, a
visitor through `/r/{code}` and a sign-up that reads the cookie, a purchase
and renewals paid up the tree, a withdrawal and the one-a-day refusal, a
refund past the balance, and the write-off. It leaves nothing behind. The
build tag is there so that installing this package never compiles it.

## Tests

```bash
GOWORK=off go test ./... -race
```

The unit suite audits the data path, holds the policy and the calculation,
and builds every published view with the view compiler an application uses —
then renders each screen with sample data. The feature suite runs the link,
the lifelong referral, renewals, every level, refunds below zero, the
write-off, one withdrawal a day under eight concurrent requests, the limits,
and tenant isolation against SQLite with arandu-wallet beside it.

## Licence

MIT. See [LICENSE.md](LICENSE.md).
