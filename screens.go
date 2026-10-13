package affiliate

import (
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/kyse/components"
	br "github.com/hyz-is/arandu-br"
	wallet "github.com/hyz-is/arandu-wallet"
)

// What the screens show, built from what the services answer. Nothing here
// reads the database or asks the policy: the handler already did both, and
// these only turn centavos, days and statuses into the text a page draws.

// brl is centavos as reais: 123456 is "R$ 1.234,56".
func brl(c int64) string { return br.BRL(c) }

// percentText is basis points as a percentage: 150 is "1,50%".
func percentText(bps int64) string {
	return strings.Replace(strconv.FormatFloat(float64(bps)/100, 'f', 2, 64), ".", ",", 1) + "%"
}

// sortCents is centavos as text that orders like the number.
func sortCents(c int64) string { return strconv.FormatInt(c+1<<62, 10) }

// parseReais reads an amount typed as reais -- "1.234,56", "1234,56",
// "1234.56", "R$ 10" -- as centavos. Two decimals of a percentage are basis
// points, so it reads a typed rate too.
func parseReais(s string) (int64, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "R$"))
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" {
		return 0, false
	}
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 || whole == "" {
		return 0, false
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > 1<<50 {
		return 0, false
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	v := w*100 + f
	if negative {
		v = -v
	}
	return v, true
}

// parseRates reads the rates of a plan's levels typed as percentages,
// separated by semicolons or by commas followed by a space: "1,5" is one
// level; "5; 2; 1" and "5, 2, 1" are three.
func parseRates(s string) ([]int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	var parts []string
	switch {
	case strings.Contains(s, ";"):
		parts = strings.Split(s, ";")
	case strings.Contains(s, ", "):
		parts = strings.Split(s, ", ")
	default:
		parts = []string{s}
	}
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, ok := parseReais(p)
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// dateTime is an instant as Brazilians write it, in a timezone.
func dateTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(loc).Format("02/01/2006 15:04")
}

// TableLines are the sentences a data table on these screens says.
func (s Screen) TableLines() components.DataTableLines {
	l := s.Labels
	return components.DataTableLines{
		Showing: l.T("table.showing"), PerPage: l.T("table.per_page"), NoResult: l.T("table.no_result"),
		Previous: l.T("table.previous"), Next: l.T("table.next"), Page: l.T("table.page"), PageOf: l.T("table.page_of"),
		Back: l.T("table.back"), Forward: l.T("table.forward"), SelectAll: l.T("table.select_all"), Bulk: l.T("table.bulk"),
		Apply: l.T("table.apply"), Ascending: l.T("table.ascending"), Descending: l.T("table.descending"), Unsorted: l.T("table.unsorted"),
	}
}

// AdminNav is the bar on top of every admin screen, the current list marked.
func (s Screen) AdminNav(current string) template.HTML {
	var b strings.Builder
	b.WriteString(`<nav class="flex flex-wrap items-center gap-1 border-b pb-3" aria-label="` + template.HTMLEscapeString(s.Labels.T("noun.affiliates")) + `">`)
	for _, item := range []struct{ key, path string }{
		{"affiliates", "/affiliates"}, {"commissions", "/commissions"}, {"plans", "/plans"}, {"withdrawals", "/withdrawals"},
	} {
		b.WriteString(string(components.Link(components.LinkProps{
			Label: s.Labels.T("noun." + item.key), URL: s.Prefix + adminSegment + item.path,
			Variant: "hover", Current: item.key == current,
			ComponentProps: components.ComponentProps{Class: "rounded-md px-3 py-1.5 text-sm"},
		})))
	}
	b.WriteString(`</nav>`)
	return template.HTML(b.String())
}

// CellInput is one field of a form drawn inside a table cell.
type CellInput struct {
	Name, Type, Placeholder, Value, Label string
	Required                              bool
}

// CellForm is a small form posted from a table cell: the CSRF token of the
// page, the fields given, and one button. Every value is escaped.
func (s Screen) CellForm(action, label, variant string, inputs ...CellInput) template.HTML {
	esc := template.HTMLEscapeString
	var b strings.Builder
	b.WriteString(`<form class="flex flex-wrap items-center gap-1" method="post" action="` + esc(action) + `">`)
	b.WriteString(`<input type="hidden" name="_token" value="` + esc(s.CSRFToken()) + `">`)
	for _, in := range inputs {
		kind := in.Type
		if kind == "" {
			kind = "text"
		}
		if kind == "hidden" {
			b.WriteString(`<input type="hidden" name="` + esc(in.Name) + `" value="` + esc(in.Value) + `">`)
			continue
		}
		b.WriteString(`<input class="input h-8 w-36 text-xs" type="` + esc(kind) + `" name="` + esc(in.Name) + `" value="` + esc(in.Value) + `"`)
		if in.Placeholder != "" {
			b.WriteString(` placeholder="` + esc(in.Placeholder) + `"`)
		}
		if in.Label != "" {
			b.WriteString(` aria-label="` + esc(in.Label) + `"`)
		}
		if in.Required {
			b.WriteString(` required`)
		}
		b.WriteString(`>`)
	}
	if variant == "" {
		variant = "outline"
	}
	b.WriteString(`<button class="btn" data-variant="` + esc(variant) + `" data-size="sm" type="submit">` + esc(label) + `</button></form>`)
	return template.HTML(b.String())
}

// options builds select options from keys, labelled from a catalogue section.
func (s Screen) options(section string, keys ...string) []components.SelectOption {
	out := make([]components.SelectOption, 0, len(keys))
	for _, k := range keys {
		out = append(out, components.SelectOption{Value: k, Label: s.Labels.T(section + "." + k)})
	}
	return out
}

// PlanKindOptions are the kinds of plan.
func (s Screen) PlanKindOptions() []components.SelectOption {
	return s.options("kind", string(Linear), string(Multilevel))
}

// LevelOptions are the levels a commission can be of.
func (s Screen) LevelOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, MaxLevels)
	for level := 1; level <= MaxLevels; level++ {
		n := strconv.Itoa(level)
		out = append(out, components.SelectOption{Value: n, Label: s.Labels.T("field.level") + " " + n})
	}
	return out
}

// CommissionStatusOptions are where a commission can stand.
func (s Screen) CommissionStatusOptions() []components.SelectOption {
	return s.options("status", string(CommissionAvailable), string(CommissionReversed))
}

// AffiliateStatusOptions are where an affiliate can stand.
func (s Screen) AffiliateStatusOptions() []components.SelectOption {
	return s.options("status", string(AffiliateActive), string(AffiliateBlocked))
}

// WithdrawalStatusOptions are where a withdrawal request can stand.
func (s Screen) WithdrawalStatusOptions() []components.SelectOption {
	return s.options("withdrawal", string(wallet.WithdrawalRequested), string(wallet.WithdrawalProcessing),
		string(wallet.WithdrawalPaid), string(wallet.WithdrawalRejected), string(wallet.WithdrawalCancelled))
}

// PayoutKindOptions are the kinds of payout account.
func (s Screen) PayoutKindOptions() []components.SelectOption {
	return s.options("payout", PayoutKindNames...)
}

// PixKeyOptions are the kinds of PIX key.
func (s Screen) PixKeyOptions() []components.SelectOption {
	return s.options("pix", "cpf", "cnpj", "email", "phone", "random")
}

// planRow is a plan as a screen shows it.
func planRow(prefix string, l Labels, p PlanView, loc *time.Location) PlanRow {
	row := PlanRow{
		ID: p.Plan.ID, Name: p.Plan.Name, Kind: l.T("kind." + string(p.Plan.Kind)), KindKey: string(p.Plan.Kind),
		Min: brl(p.Plan.MinWithdrawal), Max: "—", CookieDays: p.Plan.CookieDays, Active: bool(p.Plan.Active),
		Created: dateTime(p.Plan.CreatedAt, loc), ActivateURL: prefix + adminSegment + "/plans/" + p.Plan.ID + "/activation",
	}
	if p.Plan.MaxWithdrawal > 0 {
		row.Max = brl(p.Plan.MaxWithdrawal)
	}
	for _, level := range p.Levels {
		row.Levels = append(row.Levels, LevelRow{Level: level.Level, Rate: percentText(level.RateBps)})
	}
	return row
}

// affiliateRow is an affiliate as a list shows it.
func affiliateRow(prefix string, l Labels, a *Affiliate, loc *time.Location) AffiliateRow {
	document := br.CPF(a.Document)
	if len(a.Document) == 14 {
		document = br.CNPJ(a.Document)
	}
	return AffiliateRow{
		ID: a.ID, Name: a.Name, Code: a.Code, Document: document, User: a.UserID,
		Status: l.Status(string(a.Status)), StatusKey: string(a.Status), Active: a.IsActive(),
		Created: dateTime(a.CreatedAt, loc), URL: prefix + adminSegment + "/affiliates/" + a.ID,
	}
}

// commissionRows are a page of commissions.
func commissionRows(prefix string, l Labels, views []CommissionView, loc *time.Location) []CommissionRow {
	rows := make([]CommissionRow, 0, len(views))
	for _, v := range views {
		c := v.Commission
		row := CommissionRow{
			ID: c.ID, Date: dateTime(c.CreatedAt, loc), Level: c.Level, Rate: percentText(c.RateBps),
			Amount: brl(c.Amount), AmountSort: sortCents(c.Amount), Status: l.Status(string(c.Status)), StatusKey: string(c.Status),
			Reason: c.ReversedReason, Reversible: c.Status == CommissionAvailable,
			ReverseURL: prefix + adminSegment + "/commissions/" + c.ID + "/reversal",
		}
		if v.Conversion != nil {
			row.Order, row.Buyer, row.Product = v.Conversion.OrderRef, v.Conversion.BuyerUserID, v.Conversion.ProductID
		}
		if v.Affiliate != nil {
			row.Affiliate, row.AffiliateURL = v.Affiliate.Name, prefix+adminSegment+"/affiliates/"+v.Affiliate.ID
		}
		rows = append(rows, row)
	}
	return rows
}

// withdrawalRow is a withdrawal request as a screen shows it.
func withdrawalRow(prefix string, l Labels, r *wallet.WithdrawalRequest, a *Affiliate, loc *time.Location) WithdrawalRow {
	account := r.AccountSnapshot["label"]
	if holder := r.AccountSnapshot["holder_name"]; holder != "" {
		account += " · " + holder
	}
	base := prefix + adminSegment + "/withdrawals/" + r.ID
	row := WithdrawalRow{
		ID: r.ID, Amount: brl(int64(r.Amount)), Account: account,
		Status: l.T("withdrawal." + string(r.Status)), StatusKey: string(r.Status),
		RequestedAt: dateTime(r.RequestedAt, loc), Reason: r.RejectedReason,
		Open: r.Status == wallet.WithdrawalRequested, Processing: r.Status == wallet.WithdrawalProcessing,
		Paid:       r.Status == wallet.WithdrawalPaid,
		ProcessURL: base + "/processing", PayURL: base + "/payment", RejectURL: base + "/rejection",
	}
	if a != nil {
		row.Affiliate, row.AffiliateURL = a.Name, prefix+adminSegment+"/affiliates/"+a.ID
	}
	if row.Paid && r.ReceiptKey != "" {
		row.ReceiptURL = prefix + "/withdrawals/" + r.ID + "/receipt"
	}
	return row
}

// treeRows are an affiliate's tree as the admin reads it.
func treeRows(prefix string, l Labels, nodes []TreeNode, loc *time.Location) []TreeRow {
	rows := make([]TreeRow, 0, len(nodes))
	for _, n := range nodes {
		row := TreeRow{Level: n.Level, User: n.UserID, Name: n.Name, Code: n.Code, Joined: dateTime(n.JoinedAt, loc)}
		if n.AffiliateID != "" {
			row.Status = l.Status(string(n.Status))
			row.URL = prefix + adminSegment + "/affiliates/" + n.AffiliateID
		}
		rows = append(rows, row)
	}
	return rows
}

// pages is how many pages a total makes.
func pages(total, size int) int {
	if size <= 0 {
		return 0
	}
	return (total + size - 1) / size
}
