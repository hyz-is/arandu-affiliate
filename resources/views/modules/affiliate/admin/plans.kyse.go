//go:build kyse

package admin

import (
	"strconv"
	"strings"

	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

@go
// PlansData is what the handler hands this page.
type PlansData = affiliate.AdminPlansPageData

func plansTable(data PlansData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Plans))
	for _, p := range data.Plans {
		levels := make([]string, 0, len(p.Levels))
		for _, l := range p.Levels {
			levels = append(levels, strconv.Itoa(l.Level)+"º "+l.Rate)
		}
		state := components.Badge(components.BadgeProps{Label: data.Labels.T("field.active"), Variant: ""})
		if !p.Active {
			state = data.CellForm(p.ActivateURL, data.Labels.T("control.activate"), "outline")
		}
		rows = append(rows, components.TableRow{Key: p.ID, Cells: []components.TableCell{
			{Text: p.Name},
			{Text: p.Kind},
			{Text: strings.Join(levels, " · ")},
			{Text: strconv.Itoa(p.CookieDays)},
			{Text: p.Min},
			{Text: p.Max},
			{Text: p.Created},
			{HTML: state},
		}})
	}
	return components.DataTableProps{
		ID: "affiliate-plans", Label: data.Labels.T("screen.admin_plans_title"), Caption: data.Labels.T("screen.admin_plans_lead"),
		ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(), Complete: true, PageSize: 25,
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.name"), Key: "name", Sortable: true},
			{Label: data.Labels.T("field.kind"), Key: "kind"},
			{Label: data.Labels.T("field.levels"), Key: "levels"},
			{Label: data.Labels.T("field.cookie_days"), Key: "cookie_days", Hideable: true},
			{Label: data.Labels.T("field.min_withdrawal"), Key: "min", Hideable: true},
			{Label: data.Labels.T("field.max_withdrawal"), Key: "max", Hideable: true},
			{Label: data.Labels.T("field.created_at"), Key: "created", Hideable: true},
			{Label: data.Labels.T("field.active"), Key: "active"},
		},
		Rows:  rows,
		Empty: components.EmptyProps{Title: data.Labels.T("screen.plans_empty")},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
	{!! .AdminNav("plans") !!}

	<header class="mt-6">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.admin_plans_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.admin_plans_lead") }}</p>
	</header>

	<div class="mt-6 grid gap-6 lg:grid-cols-3">
		<div class="lg:col-span-2">{!! components.DataTable(plansTable(.)) !!}</div>

		<form class="card grid content-start gap-3 p-5" method="post" action="{{ .StoreURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.new_plan") }}</h2>
			{!! components.Field(components.FieldProps{Name: "name", Label: .Labels.T("field.name"), Required: true, Page: .Form()}) !!}
			{!! components.Select(components.SelectProps{Name: "kind", Label: .Labels.T("field.kind"), Options: .PlanKindOptions(), Required: true, Page: .Form()}) !!}
			{!! components.Field(components.FieldProps{Name: "rates", Label: .Labels.T("field.rates"), Placeholder: "1,5", Hint: .Labels.T("screen.rates_hint"), Required: true, Page: .Form()}) !!}
			<div class="grid gap-3 sm:grid-cols-2">
				{!! components.Field(components.FieldProps{Name: "min_withdrawal_brl", Label: .Labels.T("field.min_withdrawal"), Placeholder: "100,00", Page: .Form()}) !!}
				{!! components.Field(components.FieldProps{Name: "max_withdrawal_brl", Label: .Labels.T("field.max_withdrawal"), Placeholder: "0", Page: .Form()}) !!}
			</div>
			{!! components.Field(components.FieldProps{Name: "cookie_days", Label: .Labels.T("field.cookie_days"), Type: "number", Value: "30", Page: .Form()}) !!}
			<label class="flex items-center gap-2 text-sm"><input type="checkbox" class="input" name="activate" value="1" checked> {{ .Labels.T("control.activate") }}</label>
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
		</form>
	</div>
</section>
@endsection
