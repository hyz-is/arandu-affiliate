package affiliate

import "time"

// The responses of the program: declared lists of fields, never the
// entities, so a column added later does not leave through a response nobody
// reopened -- and the tenant never leaves at all.

// fieldsResource is a response whose fields the handler declared one by one.
type fieldsResource map[string]any

func (r fieldsResource) ToArray() map[string]any { return r }
func (r fieldsResource) With() map[string]any    { return nil }

func affiliateFields(a *Affiliate) map[string]any {
	return map[string]any{
		"id": a.ID, "user_id": a.UserID, "code": a.Code, "document": a.Document, "name": a.Name,
		"status": string(a.Status), "created_at": a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type affiliatesResource struct {
	rows  []*Affiliate
	total int
}

func (r affiliatesResource) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(r.rows))
	for _, a := range r.rows {
		if a != nil {
			items = append(items, affiliateFields(a))
		}
	}
	return map[string]any{"items": items, "total": r.total}
}
func (r affiliatesResource) With() map[string]any { return nil }

type plansResource []PlanView

func (r plansResource) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(r))
	for _, p := range r {
		levels := make([]map[string]any, 0, len(p.Levels))
		for _, l := range p.Levels {
			levels = append(levels, map[string]any{"level": l.Level, "rate_bps": l.RateBps})
		}
		items = append(items, map[string]any{
			"id": p.Plan.ID, "name": p.Plan.Name, "kind": string(p.Plan.Kind), "levels": levels,
			"cookie_days": p.Plan.CookieDays, "min_withdrawal": p.Plan.MinWithdrawal,
			"max_withdrawal": p.Plan.MaxWithdrawal, "active": bool(p.Plan.Active),
		})
	}
	return map[string]any{"items": items}
}
func (r plansResource) With() map[string]any { return nil }

func commissionFields(c Commission) map[string]any {
	return map[string]any{
		"id": c.ID, "conversion_id": c.ConversionID, "affiliate_id": c.AffiliateID, "level": c.Level,
		"rate_bps": c.RateBps, "amount": c.Amount, "status": string(c.Status), "reversed_reason": c.ReversedReason,
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type commissionsResource struct {
	rows  []CommissionView
	total int
}

func (r commissionsResource) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(r.rows))
	for _, v := range r.rows {
		row := commissionFields(v.Commission)
		if v.Conversion != nil {
			row["order_ref"] = v.Conversion.OrderRef
		}
		items = append(items, row)
	}
	return map[string]any{"items": items, "total": r.total}
}
func (r commissionsResource) With() map[string]any { return nil }

type conversionResource struct {
	conversion *Conversion
	paid       []*Commission
}

func (r conversionResource) ToArray() map[string]any {
	c := r.conversion
	commissions := make([]map[string]any, 0, len(r.paid))
	for _, p := range r.paid {
		commissions = append(commissions, commissionFields(*p))
	}
	return map[string]any{
		"id": c.ID, "order_ref": c.OrderRef, "buyer_user_id": c.BuyerUserID, "product_id": c.ProductID,
		"amount": c.Amount, "renewal": bool(c.Renewal), "affiliate_id": c.AffiliateID, "plan_id": c.PlanID,
		"status": string(c.Status), "occurred_at": c.OccurredAt.UTC().Format(time.RFC3339), "commissions": commissions,
	}
}
func (r conversionResource) With() map[string]any { return nil }
