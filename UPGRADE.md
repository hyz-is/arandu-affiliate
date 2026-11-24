# Upgrade Guide

## v0.5.0

### The admin held as an action, and guards in the config

Nothing is removed or renamed, and no migration is needed. A Subject that holds
`affiliate.admin` as a role is admin as in v0.4.0, and a `Config` with no guards
registers the routes as before.

What an application whose permissions are administered by arandu-permission does:

- Put `affiliate.AdminAction` (`"affiliate.admin"`) in the group of the people
  who run the program, and in the actions the application lists for its groups.
- Pass the guard that resolves a Subject's actions in `Config.Middleware` (it
  runs on every signed-in route), and the operator console's own guard, if any,
  in `Config.AdminMiddleware` (admin routes only). Without a guard that resolves
  the actions, the action is never on the request and the admin side answers 403.

## v0.4.0

### The commercial rules of a plan

Nothing is removed or renamed, and every default keeps the behaviour of v0.3.1.
Run `aru migrate`: five additive migrations add columns with neutral defaults
(`20261124_0009` to `20261124_0013`), and a plan, a conversion, a commission or a
referral written before them reads as a plan with no rule, a payment that paid
its commissions, a commission deposited at once and a referral made by link.

What an application that wants the rules does:

- Define the plan with `PlanRequest.MaxMonths`, `MaxPayments`, `AnnualBaseBps`,
  `GraceDays` and `PriorPayerMonths`; zero is off. The admin form has the fields.
- Report `ConversionRequest.Annual` for a long-period payment, and
  `BuyerDocument` and `PriorPayer` for the prior-payer rule.
- Call `Service().ReleaseDue(ctx, operator, time.Now())` from a daily job when a
  plan sets `GraceDays`. Without it a pending commission is never deposited.
- Pass `AttributeRequest.Coupon` at checkout. It prevails over `Code`, and is
  refused with `ErrCouponTooLate` once the user has a payment on record.

Three things read differently under the new code, only for a plan that uses a rule:

- A `Commission` can now be `CommissionPending` or `CommissionCancelled`, besides
  `available` and `reversed`. Code that switched on the status handles two more.
- A conversion that pays nothing because of a rule is `counted`, with
  `SkippedReason` set, and counts as a sale in `Stats.Sales`.
- `RecordRefund` and `ReverseCommission` also take the commissions that are
  pending, cancelling them.

The views change: the plan page and the affiliate's pages gain fields, columns and
two cards. A project that published them republishes:

```sh
aru vendor:publish --tag=view
aru vendor:publish --tag=view --apply
aru view:build
```

The catalogue gains the groups `unit` and the keys `field.coupon`,
`field.max_months`, `field.max_payments`, `field.annual_base`,
`field.annual_base_short`, `field.grace_days`, `field.grace`,
`field.prior_payer_months`, `field.prior_payer`, `field.duration`, `field.pending`,
`field.release_at`, `status.pending`, `status.cancelled`, `control.save_coupon` and
`screen.rules_title`, `rules_hint`, `coupon_title`, `coupon_lead`, `coupon_none`,
`coupon_admin_title`, `coupon_admin_lead`, `pending_title` and `pending_lead`, in
`en` and `pt-BR`. A line an application leaves out says the English.

The installed `aru` regenerates the queries with `ApplyListing` and `Listing`, so a
project that checks them in sees those two methods appear on each query.

### The lists are drawn as Kyse's data table now draws them

This release builds against Kyse v0.53.0, whose `DataTable` takes one `State`
where it took a query, a sort, a page and a list of chosen values. The services,
the routes and the JSON the lists answer do not change. What changes is
`TableState`, the views and the catalogue.

- `TableState` gains `Rows`, the rows of the page, `PerPage`, how many a page may
  hold, `Facets`, the values chosen for each filter (`level`, `status`), and
  `Hidden`, the keys of the columns the reader switched off (the `hide`
  parameter). It gains two methods the views hand to the table: `State()`, the
  state every address of the table is built from, and `Pager()`, the page it
  reads its position and its total from. The old fields stay and are filled as
  before.
- The three server-paged lists (the affiliate's commissions, the admin's
  commissions and the admin's affiliates) set `State`, `Paginator`, `FullPager`
  and `HeaderMenu`, and no longer set `Query`, `SearchName`, `SortKey`,
  `SortDir`, `Page`, `Pages`, `Total` or `DataTableFilter.Selected`, which Kyse
  removed. `Searchable` draws the search box where `SearchName` did.
- The four lists that hold all their rows (withdrawals of the affiliate and of
  the admin, plans, the tree of an affiliate) keep `Complete`, draw `Searchable`
  and `HeaderMenu`, and the admin's withdrawals say each row's status
  (`TableRow.Facets`) so the status filter works in the browser. The status the
  page was asked for is the table state's.
- A column that started hidden (`reason` of the affiliate's commissions, `buyer`
  of the admin's, `user` of the affiliates) starts shown, because a table with a
  state hides what the state says; the reader hides it from the columns menu.

A project that published the views republishes them:

```sh
aru vendor:publish --tag=view
aru vendor:publish --tag=view --apply
aru view:build
```

- `Screen.TableLines` fills the sentences of Kyse's new table from the `table`
  block of the catalogue. `table.showing`, `table.per_page` and `table.page` are
  no longer asked for; `table.filter`, `rows`, `rows_one`, `rows_many`,
  `selected_count`, `rows_per_page`, `first`, `last`, `select_row`, `reset`,
  `clear_filters`, `no_facet_results`, `facet_selected`, `open_menu`, `actions`,
  `toggle_columns`, `asc`, `desc` and `hide` are, and ship in `en` and `pt-BR`.
  An application that overrides the table's words in its own catalogue adds the
  new keys under `affiliate.table.`; a line it leaves out says the English.

The three server-paged lists are still searched, ordered and paged by the
services with an allowlist of sort keys, and not by a `pagination.Listing`:
moving them means a new query type for each service, which is this package's
public contract.

## v0.2.0

Go 1.26.10 is the floor: `go.mod` declares `go 1.26.10`, because every
vulnerability an Arandu application reaches is in the standard library and is
fixed there (crypto/x509, net/http HTTP/2 and CONNECT, html/template,
net/textproto, crypto/tls). A build on an older toolchain fetches 1.26.10 or
fails when `GOTOOLCHAIN=local`.

This release selects Framework v0.66.0, Hesape v0.85.0, arandu-wallet v0.20.0
and arandu-br v0.4.1; read their upgrade guides from the versions the
application required before (Framework v0.56.0, Hesape v0.54.1, arandu-wallet
v0.13.0 and arandu-br v0.2.0 in v0.1.0). Two of their changes reach this
package's screens: Hesape refuses to start a session for a subject that
carries roles or actions unless the application installed a
`SubjectResolver` with `ResolveSubjects` (the skeleton does), so the admin's
`affiliate.admin` and `wallet.operator` roles reach the handlers through the
resolver and not from the session; and Framework reads an unset `APP_ENV` as
production.

`apidiff` reports `New` and `(*Module).Routes` as changed. Their signatures
read as before; what moved is underneath the names they take: Framework
v0.63.0 declares `security.SessionStore` as an alias of hesape's
`http.SessionStore` and `fhttp.Router` as an alias of hesape's
`controllers.Router`, where v0.56.0 declared structs of its own. An application
on Framework v0.66.0 passes the store and the router it already builds and
changes nothing; one that stays on an older Framework cannot take this release.

## v0.1.0

The first release. Install arandu-wallet v0.13.0 or newer beside it, wire both
in `bootstrap/app.go` as the README shows, run `aru migrate`, publish the views
with `aru vendor:publish --tag=view --apply`, run `aru view:build`, and import
the two view directories it writes. Call `Attribute` from the sign-up and
`RecordConversion` and `RecordRefund` where payments are confirmed. Give the
admin the roles `affiliate.admin` and `wallet.operator` through
arandu-permission.
