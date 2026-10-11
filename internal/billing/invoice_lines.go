package billing

import (
	"strings"
	"time"
)

// ProrationLine is one per-item proration amount: negative for unused time on
// a removed or changed item, positive for remaining time on an added or
// changed item. Item.ID is the resolved subscription item id.
type ProrationLine struct {
	Item      LineItem
	ProductID string
	Amount    int64
}

// ProrationLines splits a subscription item change into Stripe-style per-item
// proration lines; unchanged items (same id, price and quantity) get none.
// The last line absorbs per-line rounding so the lines sum to target.
func ProrationLines(subscriptionID string, oldItems []LineItem, oldAmounts []LineAmount, newItems []LineItem, newAmounts []LineAmount, remaining, period, target int64) []ProrationLine {
	newItems = AssignSubscriptionItemIDs(subscriptionID, newItems)
	newByID := map[string]LineItem{}
	for _, item := range newItems {
		newByID[item.ID] = item
	}
	unchanged := map[string]bool{}
	var out []ProrationLine
	for idx, old := range oldItems {
		old.ID = ResolvedSubscriptionItemID(subscriptionID, old, idx)
		if next, ok := newByID[old.ID]; ok && next.PriceID == old.PriceID && next.Quantity == old.Quantity {
			unchanged[old.ID] = true
			continue
		}
		out = append(out, ProrationLine{Item: old, ProductID: lineProductAt(oldAmounts, idx), Amount: ProrateDelta(-lineAmountAt(oldAmounts, idx), remaining, period)})
	}
	for idx, item := range newItems {
		if unchanged[item.ID] {
			continue
		}
		out = append(out, ProrationLine{Item: item, ProductID: lineProductAt(newAmounts, idx), Amount: ProrateDelta(lineAmountAt(newAmounts, idx), remaining, period)})
	}
	if len(out) > 0 {
		sum := int64(0)
		for _, line := range out {
			sum += line.Amount
		}
		out[len(out)-1].Amount += target - sum
	}
	return out
}

func lineAmountAt(amounts []LineAmount, idx int) int64 {
	if idx < len(amounts) {
		return amounts[idx].Amount
	}
	return 0
}

func lineProductAt(amounts []LineAmount, idx int) string {
	if idx < len(amounts) {
		return amounts[idx].ProductID
	}
	return ""
}

// prorationInvoiceItems stores the lines of a confirmed subscription update.
// With a reset anchor the old items are credited and the new items billed for
// the new full period; otherwise each changed item gets its proration line.
// The last line absorbs rounding so the lines sum to the invoice subtotal.
func prorationInvoiceItems(invoice Invoice, old Subscription, updated Subscription, oldAmounts, newAmounts []LineAmount, remaining, period, credit int64, resetAnchor bool, at time.Time) []InvoiceItem {
	var out []InvoiceItem
	if resetAnchor {
		if credit != 0 {
			for _, line := range ProrationLines(old.ID, old.Items, oldAmounts, nil, nil, remaining, period, -credit) {
				out = append(out, subscriptionInvoiceItem(invoice, line.Item, line.ProductID, line.Amount, true, at, old.CurrentPeriodEnd, at))
			}
		}
		out = append(out, subscriptionPeriodItems(invoice, updated.Items, newAmounts, updated.CurrentPeriodStart, updated.CurrentPeriodEnd, false)...)
	} else {
		for _, line := range ProrationLines(old.ID, old.Items, oldAmounts, updated.Items, newAmounts, remaining, period, invoice.Subtotal) {
			out = append(out, subscriptionInvoiceItem(invoice, line.Item, line.ProductID, line.Amount, true, at, old.CurrentPeriodEnd, at))
		}
	}
	if len(out) > 0 {
		sum := int64(0)
		for _, item := range out {
			sum += item.Amount
		}
		out[len(out)-1].Amount += invoice.Subtotal - sum
	}
	return out
}

// subscriptionInvoiceItem is one stored invoice line for a subscription item.
func subscriptionInvoiceItem(invoice Invoice, item LineItem, productID string, amount int64, proration bool, start, end, at time.Time) InvoiceItem {
	quantity := item.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	return InvoiceItem{
		ID:                 id("ii"),
		Object:             ObjectInvoiceItem,
		CustomerID:         invoice.CustomerID,
		InvoiceID:          invoice.ID,
		SubscriptionID:     invoice.SubscriptionID,
		SubscriptionItemID: item.ID,
		Amount:             amount,
		Currency:           invoice.Currency,
		PriceID:            item.PriceID,
		ProductID:          productID,
		Quantity:           quantity,
		Proration:          proration,
		PeriodStart:        start,
		PeriodEnd:          end,
		CreatedAt:          at,
	}
}

// subscriptionPeriodItems bills every item for a full period (checkout, renewal).
func subscriptionPeriodItems(invoice Invoice, items []LineItem, amounts []LineAmount, start, end time.Time, zero bool) []InvoiceItem {
	out := make([]InvoiceItem, 0, len(items))
	for idx, item := range items {
		amount, productID := int64(0), ""
		if idx < len(amounts) {
			productID = amounts[idx].ProductID
			if !zero {
				amount = amounts[idx].Amount
			}
		}
		out = append(out, subscriptionInvoiceItem(invoice, item, productID, amount, false, start, end, invoice.CreatedAt))
	}
	return out
}

func decodeRFC3339(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return t
}
