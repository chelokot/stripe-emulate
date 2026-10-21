package api

import (
	"strconv"

	"github.com/chelokot/stripe-emulate/internal/billing"
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
type previewLineInput struct {
	id, currency, description        string
	amount, start, end, quantity     int64
	priceID                          string
	price                            map[string]any
	proration                        bool
	subscriptionID, subscriptionItem string
}

func previewLine(in previewLineInput) map[string]any {
	var pricing any
	if in.priceID != "" {
		pricing = map[string]any{
			"type":                "price_details",
			"price_details":       map[string]any{"price": in.priceID, "product": in.price["product"]},
			"unit_amount_decimal": in.price["unit_amount_decimal"],
		}
	}
	return map[string]any{
		"id":               in.id,
		"object":           "line_item",
		"amount":           in.amount,
		"currency":         in.currency,
		"description":      in.description,
		"discount_amounts": []map[string]any{},
		"discountable":     !in.proration,
		"discounts":        []any{},
		"invoice":          nil,
		"livemode":         false,
		"metadata":         map[string]string{},
		"parent": map[string]any{
			"type":                             "subscription_item_details",
			"invoice_item_details":             nil,
			"license_fee_subscription_details": nil,
			"subscription_item_details": map[string]any{
				"invoice_item":      nil,
				"proration":         in.proration,
				"proration_details": map[string]any{"credited_items": nil},
				"subscription":      in.subscriptionID,
				"subscription_item": in.subscriptionItem,
			},
		},
		"period":                map[string]any{"start": in.start, "end": in.end},
		"pretax_credit_amounts": []any{},
		"pricing":               pricing,
		"quantity":              in.quantity,
		"quantity_decimal":      strconv.FormatInt(in.quantity, 10),
		"subtotal":              in.amount,
		"taxes":                 []any{},
	}
}

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
		lines = append(lines, previewLine(previewLineInput{
			id:               "il_preview_" + sanitizeID(sub.ID) + "_" + strconv.Itoa(len(lines)),
			amount:           line.Amount,
			currency:         in.currency,
			description:      description,
			start:            in.start,
			end:              in.end,
			quantity:         quantity,
			priceID:          item.PriceID,
			price:            prices[item.PriceID],
			proration:        true,
			subscriptionID:   sub.ID,
			subscriptionItem: item.ID,
		}))
	}
	if len(lines) > 0 {
		if discountLine < 0 {
			discountLine = 0
		}
		lines[discountLine]["discount_amounts"] = in.discounts
	}
	return lines
}
