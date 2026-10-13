//go:build kyse

package admin

import (
	"html/template"

	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

@go
// WithdrawalsData is what the handler hands this page.
type WithdrawalsData = affiliate.AdminWithdrawalsPageData

func withdrawalVariant(key string) string {
	switch key {
	case "requested":
		return ""
	case "rejected", "cancelled":
		return "destructive"
	case "paid":
		return "outline"
	}
	return "secondary"
}

func withdrawalsTable(data WithdrawalsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, w := range data.Rows {
		status := components.Badge(components.BadgeProps{Label: w.Status, Variant: withdrawalVariant(w.StatusKey)})
		if w.ReceiptURL != "" {
			status += template.HTML(" ") + components.Link(components.LinkProps{Label: data.Labels.T("field.receipt"), URL: w.ReceiptURL, Variant: "hover"})
		}
		rows = append(rows, components.TableRow{Key: w.ID, Cells: []components.TableCell{
			{Text: w.RequestedAt},
			{Text: w.Affiliate},
			{Text: w.Amount},
			{Text: w.Account},
			{HTML: status, SortValue: w.StatusKey},
			{Text: w.Reason},
		}})
	}
	var selected []string
	if data.Status != "" {
		selected = []string{data.Status}
	}
	return components.DataTableProps{
		ID: "affiliate-withdrawals", Label: data.Labels.T("screen.admin_withdrawals_title"), URL: data.URL,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 25,
		SearchName: "q", SearchPlaceholder: data.Labels.T("control.search"),
		Filters: []components.DataTableFilter{{Key: "status", Label: data.Labels.T("field.status"), Options: data.WithdrawalStatusOptions(), Selected: selected}},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.requested_at"), Key: "requested_at", Sortable: true},
			{Label: data.Labels.T("noun.affiliate"), Key: "affiliate", Sortable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.account"), Key: "account", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.withdrawals_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("withdrawals") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_withdrawals_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_withdrawals_lead") }}</p>
	</header>

	<ul class="mt-6 grid gap-4 lg:grid-cols-2">
	@foreach(.Rows as row)
		@if(row.Open || row.Processing)
			<li class="card grid gap-4 p-5">
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div>
						<p class="text-sm font-semibold">{{ row.Affiliate }}</p>
						<p class="text-muted-foreground text-xs">{{ row.RequestedAt }} · {{ row.Account }}</p>
					</div>
					<p class="text-xl font-semibold tabular-nums">{{ row.Amount }}</p>
				</div>
				@if(row.Open)
					<form method="post" action="{{ row.ProcessURL }}">
						@csrf
						{!! components.Button(components.ButtonProps{Label: .Labels.T("control.process"), Type: "submit", Size: "sm"}) !!}
					</form>
				@endif
				@if(row.Processing)
					<form class="grid gap-3" method="post" action="{{ row.PayURL }}" enctype="multipart/form-data">
						@csrf
						{!! components.Field(components.FieldProps{Name: "end_to_end_id", Label: .Labels.T("field.end_to_end_id"), Required: true}) !!}
						<label class="label">{{ .Labels.T("field.receipt") }}
							<input class="input mt-1" type="file" name="receipt" accept="application/pdf,image/jpeg,image/png" required>
						</label>
						<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.pay"), Type: "submit", Size: "sm"}) !!}</div>
					</form>
				@endif
				<form class="flex flex-wrap items-end gap-2 border-t pt-3" method="post" action="{{ row.RejectURL }}">
					@csrf
					<div class="grow">{!! components.Field(components.FieldProps{Name: "reason", Label: .Labels.T("field.reason"), Required: true}) !!}</div>
					{!! components.Button(components.ButtonProps{Label: .Labels.T("control.reject"), Type: "submit", Variant: "destructive", Size: "sm"}) !!}
				</form>
			</li>
		@endif
	@endforeach
	</ul>

	<div class="mt-8">{!! components.DataTable(withdrawalsTable(.)) !!}</div>
</section>
@endsection
