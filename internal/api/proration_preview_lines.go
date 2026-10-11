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

// prorationPreviewLines builds Stripe-style per-item proration lines: an
// "Unused time" credit for each removed or changed item and a "Remaining time"
// charge for each added or changed item; unchanged items get no line. Each line
// names its subscription_item so clients can split credits per item. Lines are
// gross; the last one absorbs per-line rounding so they sum to the subtotal.
func prorationPreviewLines(in prorationPreviewInput) []map[string]any {
	sub := in.subscription
	newItems := billing.AssignSubscriptionItemIDs(sub.ID, in.newItems)
	newByID := map[string]billing.LineItem{}
	for _, item := range newItems {
		newByID[item.ID] = item
	}
	unchanged := map[string]bool{}
	lines := []map[string]any{}
	add := func(item billing.LineItem, itemID string, amount int64, description string, prices map[string]map[string]any) {
		quantity := item.Quantity
		if quantity <= 0 {
			quantity = 1
		}
		var price any
		if payload, ok := prices[item.PriceID]; ok {
			price = payload
		}
		lines = append(lines, map[string]any{
			"id":               "il_preview_" + sanitizeID(sub.ID) + "_" + strconv.Itoa(len(lines)),
			"object":           "line_item",
			"amount":           amount,
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
					"subscription_item": itemID,
				},
			},
		})
	}
	for idx, old := range sub.Items {
		itemID := billing.ResolvedSubscriptionItemID(sub.ID, old, idx)
		if next, ok := newByID[itemID]; ok && next.PriceID == old.PriceID && next.Quantity == old.Quantity {
			unchanged[itemID] = true
			continue
		}
		amount := int64(0)
		if idx < len(in.oldAmounts) {
			amount = in.oldAmounts[idx].Amount
		}
		add(old, itemID, billing.ProrateDelta(-amount, in.remaining, in.period), "Unused time after subscription update", in.oldPrices)
	}
	for idx, item := range newItems {
		if unchanged[item.ID] {
			continue
		}
		amount := int64(0)
		if idx < len(in.newAmounts) {
			amount = in.newAmounts[idx].Amount
		}
		add(item, item.ID, billing.ProrateDelta(amount, in.remaining, in.period), "Remaining time after subscription update", in.newPrices)
	}
	if len(lines) == 0 {
		return lines
	}
	sum := int64(0)
	discountLine := -1
	for idx, line := range lines {
		amount := line["amount"].(int64)
		sum += amount
		if discountLine < 0 && amount > 0 {
			discountLine = idx
		}
	}
	last := lines[len(lines)-1]
	last["amount"] = last["amount"].(int64) + in.subtotal - sum
	if discountLine < 0 {
		discountLine = 0
	}
	lines[discountLine]["discount_amounts"] = in.discounts
	return lines
}
