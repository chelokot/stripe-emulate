package api

import (
	"strconv"

	"github.com/midagedev/billtap/internal/billing"
)

type prorationPreviewInput struct {
	subscription         billing.Subscription
	oldPrices, newPrices map[string]map[string]any
	oldAmounts           []billing.LineAmount
	newItems             []billing.LineItem
	newAmounts           []billing.LineAmount
	remaining, period    int64
	subtotal             int64
	discounts            []map[string]any
	currency             string
	start, end           int64
}

// prorationPreviewLines renders billing.ProrationLines (the same split a
// confirmed update stores) as preview line items. Each names its
// subscription_item so clients can split credits per item; invoice-level
// discount amounts go on the first charge line.
func prorationPreviewLines(in prorationPreviewInput) []map[string]any {
	sub := in.subscription
	lines := []map[string]any{}
	discountLine := -1
	for _, line := range billing.ProrationLines(sub.ID, sub.Items, in.oldAmounts, in.newItems, in.newAmounts, in.remaining, in.period, in.subtotal) {
		item := line.Item
		quantity := item.Quantity
		if quantity <= 0 {
			quantity = 1
		}
		prices, description := in.newPrices, "Remaining time after subscription update"
		if line.Amount < 0 {
			prices, description = in.oldPrices, "Unused time after subscription update"
		} else if discountLine < 0 {
			discountLine = len(lines)
		}
		var price any
		if payload, ok := prices[item.PriceID]; ok {
			price = payload
		}
		lines = append(lines, map[string]any{
			"id":               "il_preview_" + sanitizeID(sub.ID) + "_" + strconv.Itoa(len(lines)),
			"object":           "line_item",
			"amount":           line.Amount,
			"currency":         in.currency,
			"description":      description,
			"discount_amounts": []map[string]any{},
			"discountable":     false,
			"period":           map[string]any{"start": in.start, "end": in.end},
			"proration":        true,
			"price":            price,
			"quantity":         quantity,
			"subscription":     sub.ID,
			"type":             "invoiceitem",
			"parent": map[string]any{
				"type": "subscription_item_details",
				"subscription_item_details": map[string]any{
					"price":             item.PriceID,
					"proration":         true,
					"subscription":      sub.ID,
					"subscription_item": item.ID,
				},
			},
		})
	}
	if len(lines) > 0 {
		if discountLine < 0 {
			discountLine = 0
		}
		lines[discountLine]["discount_amounts"] = in.discounts
	}
	return lines
}
