//go:build kyse

package affiliate

import (
	"html/template"
	"strconv"

	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

@go
// DashboardData is what the handler hands this page.
//
// It is an alias rather than a struct declared here, so the shape is written
// once, beside the code that fills it.
type DashboardData = affiliate.DashboardPageData

func badgeOf(key string) string {
	switch key {
	case "available", "active", "requested":
		return ""
	case "reversed", "rejected", "blocked", "cancelled":
		return "destructive"
	case "paid":
		return "outline"
	}
	return "secondary"
}

func commissionsTable(data DashboardData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, c := range data.Rows {
		status := components.Badge(components.BadgeProps{Label: c.Status, Variant: badgeOf(c.StatusKey)})
		rows = append(rows, components.TableRow{Key: c.ID, Cells: []components.TableCell{
			{Text: c.Date},
			{Text: strconv.Itoa(c.Level)},
			{Text: c.Product},
			{Text: c.Rate},
			{Text: c.Amount},
			{HTML: status},
			{Text: c.Reason},
		}})
	}
	var levels, statuses []string
	if data.Level != "" {
		levels = []string{data.Level}
	}
	if data.Status != "" {
		statuses = []string{data.Status}
	}
	return components.DataTableProps{
		ID: "affiliate-commissions", Label: data.Labels.T("screen.commissions_title"), URL: data.Table.URL,
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Filters: []components.DataTableFilter{
			{Key: "level", Label: data.Labels.T("field.level"), Options: data.LevelOptions(), Selected: levels},
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.CommissionStatusOptions(), Selected: statuses},
		},
		SortKey: data.Table.Sort, SortDir: data.Table.Dir,
		Page: data.Table.CurrentPage, Pages: data.Table.Pages, Total: data.Table.Total,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.date"), Key: "created_at", Sortable: true},
			{Label: data.Labels.T("field.level"), Key: "level", Sortable: true},
			{Label: data.Labels.T("field.product"), Key: "product", Hideable: true},
			{Label: data.Labels.T("field.rate"), Key: "rate", Hideable: true},
			{Label: data.Labels.T("field.amount"), Key: "amount", Sortable: true, Align: "right"},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true, Hidden: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.commissions_empty")},
	}
}

func withdrawalsTable(data DashboardData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Withdrawals))
	for _, w := range data.Withdrawals {
		status := components.Badge(components.BadgeProps{Label: w.Status, Variant: badgeOf(w.StatusKey)})
		if w.ReceiptURL != "" {
			status += template.HTML(" ") + components.Link(components.LinkProps{Label: data.Labels.T("field.receipt"), URL: w.ReceiptURL, Variant: "hover"})
		}
		rows = append(rows, components.TableRow{Key: w.ID, Cells: []components.TableCell{
			{Text: w.RequestedAt},
			{Text: w.Amount},
			{Text: w.Account},
			{HTML: status},
			{Text: w.Reason},
		}})
	}
	return components.DataTableProps{
		ID: "affiliate-withdrawals", Label: data.Labels.T("screen.withdrawals_title"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 10,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.requested_at"), Key: "requested_at"},
			{Label: data.Labels.T("field.amount"), Key: "amount", Align: "right"},
			{Label: data.Labels.T("field.account"), Key: "account", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status"},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.withdrawals_empty")},
	}
}

func accountOptions(data DashboardData) []components.SelectOption {
	out := make([]components.SelectOption, 0, len(data.Accounts))
	for _, a := range data.Accounts {
		out = append(out, components.SelectOption{Value: a.ID, Label: a.Label})
	}
	return out
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<p class="text-muted-foreground text-xs font-medium uppercase tracking-wide">{{ .Labels.T("noun.affiliate") }}</p>
			<h1 class="mt-1 text-2xl font-semibold tracking-tight">{{ .Name }}</h1>
		</div>
		<p class="text-muted-foreground max-w-prose text-sm">{{ .Labels.T("screen.dashboard_lead") }}</p>
	</header>

	@if(.Blocked)
		<div class="alert mt-6" data-variant="destructive" role="alert"><p class="text-sm">{{ .Labels.T("screen.blocked_note") }}</p></div>
	@endif

	<div class="card mt-6 grid gap-3 p-5">
		<h2 class="text-base font-semibold">{{ .Labels.T("screen.link_title") }}</h2>
		<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.link_lead") }}</p>
		<div class="flex flex-wrap items-center gap-2">
			<code id="affiliate-link" class="bg-muted rounded-md px-3 py-2 font-mono text-sm">{{ .Link }}</code>
			{!! components.CopyButton(components.CopyButtonProps{
				Value: .Link, SourceID: "affiliate-link",
				Label: .Labels.T("control.copy"), CopiedLabel: .Labels.T("control.copied"), Variant: "outline", Size: "sm",
			}) !!}
		</div>
	</div>

	<form class="mt-8 flex flex-wrap items-end gap-3" method="get" action="{{ .Table.URL }}">
		<h2 class="mr-auto text-lg font-semibold">{{ .Labels.T("screen.numbers_title") }}</h2>
		<div>{!! components.Field(components.FieldProps{Name: "from", Label: .Labels.T("field.from"), Type: "date", Value: .From}) !!}</div>
		<div>{!! components.Field(components.FieldProps{Name: "to", Label: .Labels.T("field.to"), Type: "date", Value: .To}) !!}</div>
		{!! components.Button(components.ButtonProps{Label: .Labels.T("control.filter"), Type: "submit", Variant: "outline"}) !!}
	</form>
	<dl class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.clicks") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Clicks }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.signups") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Signups }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.sales") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Sales }}</dd></div>
		<div class="card p-4"><dt class="text-muted-foreground text-xs">{{ .Labels.T("field.commissions") }}</dt><dd class="mt-1 text-xl font-semibold tabular-nums">{{ .Commissions }}</dd></div>
	</dl>

	<div class="mt-8 grid gap-6 lg:grid-cols-3">
		<div class="card grid content-start gap-3 p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.balance_title") }}</h2>
			<dl class="grid gap-2 text-sm">
				<div class="flex justify-between"><dt class="text-muted-foreground">{{ .Labels.T("field.available") }}</dt><dd class="text-lg font-semibold tabular-nums">{{ .Available }}</dd></div>
				<div class="flex justify-between"><dt class="text-muted-foreground">{{ .Labels.T("field.held") }}</dt><dd class="tabular-nums">{{ .Held }}</dd></div>
				<div class="flex justify-between"><dt class="text-muted-foreground">{{ .Labels.T("field.balance") }}</dt><dd class="tabular-nums">{{ .Balance }}</dd></div>
			</dl>
			@if(.Negative)
				<p class="text-destructive text-sm">{{ .Labels.T("screen.negative_balance") }}</p>
			@endif
		</div>

		<div class="card grid content-start gap-3 p-5">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.plan_title") }}</h2>
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.plan_lead") }}</p>
			@if(.Plan != nil)
				<p class="text-sm font-medium">{{ .Plan.Name }} · {{ .Plan.Kind }}</p>
				<ul class="grid gap-1 text-sm">
				@foreach(.Plan.Levels as level)
					<li class="flex justify-between"><span>{{ .Labels.T("field.level") }} {{ level.Level }}</span><span class="font-semibold tabular-nums">{{ level.Rate }}</span></li>
				@endforeach
				</ul>
			@else
				<p class="text-sm">{{ .Labels.T("screen.plan_none") }}</p>
			@endif
			<p class="text-muted-foreground text-xs">{{ .Labels.T("screen.limits") }}: {{ .Min }} – {{ .Max }}</p>
		</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .WithdrawURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.withdraw_title") }}</h2>
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.withdraw_lead") }}</p>
			@if(len(.Accounts) == 0)
				<p class="text-sm">{{ .Labels.T("control.no_account") }}</p>
			@else
				{!! components.Field(components.FieldProps{Name: "amount_brl", Label: .Labels.T("field.amount_brl"), Placeholder: .Available, Page: .Form()}) !!}
				{!! components.Select(components.SelectProps{
					Name: "account_id", Label: .Labels.T("field.account"), Options: accountOptions(.),
					Placeholder: .Labels.T("control.choose_account"), Required: true, Page: .Form(),
				}) !!}
				<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.withdraw"), Type: "submit"}) !!}</div>
			@endif
		</form>
	</div>

	<div class="mt-10">{!! components.DataTable(commissionsTable(.)) !!}</div>

	<div class="mt-10 grid gap-6 lg:grid-cols-3">
		<div class="lg:col-span-2">{!! components.DataTable(withdrawalsTable(.)) !!}</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .AccountURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.account_title") }}</h2>
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.account_lead") }}</p>
			{!! components.Select(components.SelectProps{Name: "kind", Label: .Labels.T("field.account_kind"), Options: .PayoutKindOptions(), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "label", Label: .Labels.T("field.label"), Page: .Form()}) !!}
			<fieldset class="grid gap-2">
				<legend class="text-muted-foreground text-xs">{{ .Labels.T("payout.pix") }}</legend>
				{!! components.Select(components.SelectProps{Name: "pix_key_type", Label: .Labels.T("field.pix_key_type"), Options: .PixKeyOptions(), Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "pix_key", Label: .Labels.T("field.pix_key"), Page: .Form()}) !!}
			</fieldset>
			<fieldset class="grid gap-2">
				<legend class="text-muted-foreground text-xs">{{ .Labels.T("payout.bank") }}</legend>
				{!! components.Field(components.FieldProps{Name: "bank", Label: .Labels.T("field.bank"), Page: .Form()}) !!}
				<div class="grid grid-cols-2 gap-2">
					{!! components.Field(components.FieldProps{Name: "agency", Label: .Labels.T("field.agency"), Page: .Form()}) !!}
					{!! components.Field(components.FieldProps{Name: "account_number", Label: .Labels.T("field.account_number"), Page: .Form()}) !!}
				</div>
				{!! components.Field(components.FieldProps{Name: "account_type", Label: .Labels.T("field.account_type"), Page: .Form()}) !!}
			</fieldset>
			<fieldset class="grid gap-2">
				<legend class="text-muted-foreground text-xs">{{ .Labels.T("payout.crypto") }}</legend>
				{!! components.Field(components.FieldProps{Name: "network", Label: .Labels.T("field.network"), Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "address", Label: .Labels.T("field.address"), Page: .Form()}) !!}
			</fieldset>
			<fieldset class="grid gap-2">
				<legend class="text-muted-foreground text-xs">{{ .Labels.T("payout.other") }}</legend>
				{!! components.Field(components.FieldProps{Name: "description", Label: .Labels.T("field.description"), Page: .Form()}) !!}
			</fieldset>
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit", Variant: "outline"}) !!}</div>
		</form>
	</div>
</section>
@endsection
