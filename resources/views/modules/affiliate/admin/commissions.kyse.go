//go:build kyse

package admin

import (
	"html/template"
	"strconv"

	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

@go
// CommissionsData is what the handler hands this page.
type CommissionsData = affiliate.AdminCommissionsPageData

func commissionsTable(data CommissionsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, c := range data.Rows {
		variant := ""
		if !c.Reversible {
			variant = "destructive"
		}
		action := template.HTML("")
		if c.Reversible {
			action = data.CellForm(c.ReverseURL, data.Labels.T("control.reverse"), "outline",
				affiliate.CellInput{Name: "reason", Placeholder: data.Labels.T("field.reason"), Label: data.Labels.T("field.reason"), Required: true})
		}
		who := template.HTML(template.HTMLEscapeString(c.Affiliate))
		if c.AffiliateURL != "" {
			who = components.Link(components.LinkProps{Label: c.Affiliate, URL: c.AffiliateURL, Variant: "hover"})
		}
		rows = append(rows, components.TableRow{Key: c.ID, Cells: []components.TableCell{
			{Text: c.Date},
			{HTML: who},
			{Text: strconv.Itoa(c.Level)},
			{Text: c.Order},
			{Text: c.Buyer},
			{Text: c.Rate},
			{Text: c.Amount},
			{HTML: components.Badge(components.BadgeProps{Label: c.Status, Variant: variant})},
			{Text: c.ReleaseAt},
			{Text: c.Reason},
			{HTML: action},
		}})
	}
	return components.DataTableProps{
		ID: "affiliate-admin-commissions", Label: data.Labels.T("screen.admin_commissions_title"), URL: data.Table.URL,
		State: data.Table.State(), Paginator: data.Table.Pager(), FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.admin_commissions_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Filters: []components.DataTableFilter{
			{Key: "level", Label: data.Labels.T("field.level"), Options: data.LevelOptions()},
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.CommissionStatusOptions()},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.date"), Key: "created_at", Sortable: true},
			{Label: data.Labels.T("noun.affiliate"), Key: "affiliate"},
			{Label: data.Labels.T("field.level"), Key: "level", Sortable: true},
			{Label: data.Labels.T("field.order"), Key: "order", Hideable: true},
			{Label: data.Labels.T("field.buyer"), Key: "buyer", Hideable: true},
			{Label: data.Labels.T("field.rate"), Key: "rate", Hideable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Sortable: true, Align: "right"},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.release_at"), Key: "release_at", Hideable: true},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true},
			{Label: "", Key: "reverse"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.commissions_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("commissions") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_commissions_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_commissions_lead") }}</p>
	</header>

	<div class="mt-6">{!! components.DataTable(commissionsTable(.)) !!}</div>
</section>
@endsection
