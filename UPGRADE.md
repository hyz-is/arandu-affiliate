# Upgrade Guide

## Unreleased

## v0.1.0

The first release. Install arandu-wallet v0.13.0 or newer beside it, wire both
in `bootstrap/app.go` as the README shows, run `aru migrate`, publish the views
with `aru vendor:publish --tag=view --apply`, run `aru view:build`, and import
the two view directories it writes. Call `Attribute` from the sign-up and
`RecordConversion` and `RecordRefund` where payments are confirmed. Give the
admin the roles `affiliate.admin` and `wallet.operator` through
arandu-permission.
