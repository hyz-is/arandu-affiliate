//go:build kyse

package admin

import (
	"html/template"
	"strconv"

	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

@go
// AffiliateData is what the handler hands this page.
type AffiliateData = affiliate.AdminAffiliatePageData

func treeTable(data AffiliateData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Tree))
	for i, n := range data.Tree {
		name := template.HTML(template.HTMLEscapeString(n.User))
		if n.URL != "" {
			name = components.Link(components.LinkProps{Label: n.Name + " (" + n.Code + ")", URL: n.URL, Variant: "hover"})
		}
		rows = append(rows, components.TableRow{Key: strconv.Itoa(i), Cells: []components.TableCell{
			{Text: strconv.Itoa(n.Level), SortValue: strconv.Itoa(n.Level)},
			{HTML: name},
			{Text: n.User},
			{Text: n.Status},
			{Text: n.Joined},
		}})
	}
	return components.DataTableProps{
		ID: "affiliate-tree", Label: data.Labels.T("screen.tree_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 25,
		SearchName: "q", SearchPlaceholder: data.Labels.T("control.search"),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.level"), Key: "level", Sortable: true},
			{Label: data.Labels.T("noun.affiliate"), Key: "name"},
			{Label: data.Labels.T("field.user"), Key: "user", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Hideable: true},
			{Label: data.Labels.T("field.joined_at"), Key: "joined", Sortable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.tree_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("affiliates") !!}

	<header class="mt-6 flex flex-wrap items-start justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Affiliate.Document }} · {{ .Affiliate.User }}</p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Affiliate.Name }}</h1>
			<p class="text-muted-foreground mt-1 font-mono text-xs">{{ .Link }}</p>
		</div>
		<div class="flex items-center gap-2">
			@if(.Affiliate.Active)
				{!! components.Badge(components.BadgeProps{Label: .Affiliate.Status}) !!}
				<form method="post" action="{{ .BlockURL }}">
					@csrf
					{!! components.Button(components.ButtonProps{Label: .Labels.T("control.block"), Type: "submit", Variant: "destructive", Size: "sm"}) !!}
				</form>
			@else
				{!! components.Badge(components.BadgeProps{Label: .Affiliate.Status, Variant: "destructive"}) !!}
				<form method="post" action="{{ .BlockURL }}">
					@csrf
					<input type="hidden" name="_method" value="DELETE">
					{!! components.Button(components.ButtonProps{Label: .Labels.T("control.unblock"), Type: "submit", Size: "sm"}) !!}
				</form>
			@endif
			<a class="btn" data-variant="outline" data-size="sm" href="{{ .CommissionsURL }}">{{ .Labels.T("noun.commissions") }}</a>
		</div>
	</header>

	<dl class="mt-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.balance") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Balance }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.held") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Held }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.clicks") }} · {{ .Labels.T("field.signups") }} · {{ .Labels.T("field.sales") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Clicks }} · {{ .Signups }} · {{ .Sales }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.commissions") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Commissions }}</dd></div>
	</dl>

	@if(.Negative)
		<form class="card mt-6 flex flex-wrap items-end gap-3 p-5" method="post" action="{{ .WriteOffURL }}">
			@csrf
			<div class="grow">
				<h2 class="text-base font-semibold">{{ .Labels.T("screen.write_off_title") }}</h2>
				<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.write_off_lead") }}</p>
				{!! components.Field(components.FieldProps{Name: "reason", Label: .Labels.T("field.reason"), Required: true, Page: .Form()}) !!}
			</div>
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.write_off"), Type: "submit", Variant: "outline"}) !!}
		</form>
	@endif

	<div class="mt-8">{!! components.DataTable(treeTable(.)) !!}</div>
</section>
@endsection
