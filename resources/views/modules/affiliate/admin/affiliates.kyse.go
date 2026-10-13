//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

@go
// AffiliatesData is what the handler hands this page.
type AffiliatesData = affiliate.AdminAffiliatesPageData

// affiliatesTable is the list, paged, searched, filtered and ordered by the
// server.
func affiliatesTable(data AffiliatesData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, a := range data.Rows {
		variant := "destructive"
		if a.Active {
			variant = ""
		}
		rows = append(rows, components.TableRow{Key: a.ID, Cells: []components.TableCell{
			{HTML: components.Link(components.LinkProps{Label: a.Name, URL: a.URL, Variant: "hover"})},
			{Text: a.Code},
			{Text: a.Document},
			{Text: a.User},
			{HTML: components.Badge(components.BadgeProps{Label: a.Status, Variant: variant})},
			{Text: a.Created},
		}})
	}
	var statuses []string
	if data.Status != "" {
		statuses = []string{data.Status}
	}
	return components.DataTableProps{
		ID: "affiliate-registry", Label: data.Labels.T("screen.admin_affiliates_title"), URL: data.Table.URL,
		Caption: data.Labels.T("screen.admin_affiliates_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Query: data.Table.Query, SearchName: "q", SearchPlaceholder: data.Labels.T("control.search"),
		Filters: []components.DataTableFilter{{Key: "status", Label: data.Labels.T("field.status"), Options: data.AffiliateStatusOptions(), Selected: statuses}},
		SortKey: data.Table.Sort, SortDir: data.Table.Dir,
		Page: data.Table.CurrentPage, Pages: data.Table.Pages, Total: data.Table.Total,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.name"), Key: "name", Sortable: true},
			{Label: data.Labels.T("field.code"), Key: "code", Sortable: true},
			{Label: data.Labels.T("field.document"), Key: "document", Hideable: true},
			{Label: data.Labels.T("field.user"), Key: "user", Hideable: true, Hidden: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.created_at"), Key: "created_at", Sortable: true, Hideable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.affiliates_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("affiliates") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_affiliates_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_affiliates_lead") }}</p>
	</header>

	<div class="mt-6 grid gap-6 lg:grid-cols-3">
		<div class="lg:col-span-2">{!! components.DataTable(affiliatesTable(.)) !!}</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .StoreURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.new_affiliate") }}</h2>
			{!! components.Field(components.FieldProps{Name: "name", Label: .Labels.T("field.name"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "document", Label: .Labels.T("field.document"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "user_id", Label: .Labels.T("field.user"), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "code", Label: .Labels.T("field.code"), Hint: .Labels.T("screen.code_hint"), Page: .Form()}) !!}
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
		</form>
	</div>
</section>
@endsection
