# Upgrade Guide

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
