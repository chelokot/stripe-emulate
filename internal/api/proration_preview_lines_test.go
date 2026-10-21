package api

import (
	"fmt"
	"net/url"
	"strconv"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

// Plan swap + seat change: each changed item gets its own credit/charge line
// naming its subscription_item, so a client can split plan vs seat credits.
func TestCreatePreviewProrationLinesPerSubscriptionItem(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"split@example.test"}})
	product := postForm[billing.Product](t, handler, "/v1/products", url.Values{"name": {"Workspace"}})
	price := func(amount string) string {
		return postForm[billing.Price](t, handler, "/v1/prices", url.Values{"product": {product.ID}, "currency": {"usd"}, "unit_amount": {amount}, "recurring[interval]": {"month"}}).ID
	}
	basic, pro, seat := price("1000"), price("3000"), price("500")
	sub := postForm[prorationSubResponse](t, handler, "/v1/subscriptions", url.Values{
		"customer": {customer.ID}, "items[0][price]": {basic}, "items[1][price]": {seat}, "items[1][quantity]": {"2"},
	})
	planItem, seatItem := sub.Items.Data[0].ID, sub.Items.Data[1].ID

	preview := postForm[map[string]any](t, handler, "/v1/invoices/create_preview", url.Values{
		"subscription":                             {sub.ID},
		"subscription_details[items][0][id]":       {planItem},
		"subscription_details[items][0][price]":    {pro},
		"subscription_details[items][1][id]":       {seatItem},
		"subscription_details[items][1][price]":    {seat},
		"subscription_details[items][1][quantity]": {"5"},
		"subscription_details[proration_date]":     {strconv.FormatInt(sub.CurrentPeriodStart, 10)},
	})
	lines, _ := preview["lines"].(map[string]any)["data"].([]any)
	byItem := map[string]int64{}
	for _, raw := range lines {
		line := raw.(map[string]any)
		details := line["parent"].(map[string]any)["subscription_item_details"].(map[string]any)
		byItem[fmt.Sprint(details["subscription_item"])] += int64(line["amount"].(float64))
	}
	// Full period remaining: plan 3000-1000, seats 5*500-2*500.
	if len(lines) != 4 || byItem[planItem] != 2000 || byItem[seatItem] != 1500 {
		t.Fatalf("lines=%d per-item net=%v, want 4 lines, plan=2000 seat=1500", len(lines), byItem)
	}
	if subtotal := int64(preview["subtotal"].(float64)); subtotal != 3500 {
		t.Fatalf("subtotal=%d, want 3500", subtotal)
	}
}

// Subscription invoices store their lines: create bills each item for the
// period, an always_invoice upgrade stores per-item credit/charge lines. Each
// line has period, parent.subscription_item_details and its discount share.
func TestSubscriptionInvoiceLinesCarryPeriodParentAndDiscount(t *testing.T) {
	handler := newTestHandler(t)
	customer, lite, pro, _ := setupProrationPlans(t, handler)
	coupon := postForm[map[string]any](t, handler, "/v1/coupons", url.Values{"percent_off": {"25"}, "duration": {"forever"}})
	created := postForm[prorationSubResponse](t, handler, "/v1/subscriptions", url.Values{
		"customer": {customer.ID}, "items[0][price]": {lite.ID}, "coupon": {fmt.Sprint(coupon["id"])},
	})
	itemID := created.Items.Data[0].ID

	type line struct {
		Amount          int64 `json:"amount"`
		Proration       bool  `json:"proration"`
		Period          struct{ Start, End int64 }
		DiscountAmounts []struct {
			Amount int64 `json:"amount"`
		} `json:"discount_amounts"`
		Parent struct {
			Type                    string `json:"type"`
			SubscriptionItemDetails struct {
				Subscription     string `json:"subscription"`
				SubscriptionItem string `json:"subscription_item"`
			} `json:"subscription_item_details"`
		} `json:"parent"`
	}
	linesOf := func(invoiceID string) []line {
		inv := getJSON[struct {
			Lines struct {
				Data []line `json:"data"`
			} `json:"lines"`
		}](t, handler, "/v1/invoices/"+invoiceID)
		for _, l := range inv.Lines.Data {
			if l.Parent.Type != "subscription_item_details" || l.Parent.SubscriptionItemDetails.Subscription != created.ID || l.Parent.SubscriptionItemDetails.SubscriptionItem != itemID {
				t.Fatalf("line parent=%#v, want subscription_item_details for %s/%s", l.Parent, created.ID, itemID)
			}
		}
		return inv.Lines.Data
	}

	first := linesOf(created.LatestInvoice)
	if len(first) != 1 || first[0].Amount != 4900 || first[0].Proration || len(first[0].DiscountAmounts) != 1 || first[0].DiscountAmounts[0].Amount != 1225 {
		t.Fatalf("create lines=%#v, want one 4900 line with 1225 discount", first)
	}
	if first[0].Period.Start != created.CurrentPeriodStart || first[0].Period.End != created.CurrentPeriodEnd {
		t.Fatalf("create line period=%#v, want subscription period %d..%d", first[0].Period, created.CurrentPeriodStart, created.CurrentPeriodEnd)
	}

	upgraded := postForm[prorationSubResponse](t, handler, "/v1/subscriptions/"+created.ID, url.Values{
		"items[0][id]":       {itemID},
		"items[0][price]":    {pro.ID},
		"proration_behavior": {"always_invoice"},
		"proration_date":     {strconv.FormatInt(created.CurrentPeriodStart, 10)},
	})
	upgrade := linesOf(upgraded.LatestInvoice)
	if len(upgrade) != 2 || upgrade[0].Amount != -4900 || upgrade[1].Amount != 9900 || !upgrade[0].Proration || !upgrade[1].Proration {
		t.Fatalf("upgrade lines=%#v, want -4900 credit and 9900 charge prorations", upgrade)
	}
	if len(upgrade[1].DiscountAmounts) != 1 || upgrade[1].DiscountAmounts[0].Amount != 1250 || upgrade[1].Period.End != created.CurrentPeriodEnd {
		t.Fatalf("upgrade charge line=%#v, want 1250 discount and period ending %d", upgrade[1], created.CurrentPeriodEnd)
	}
}

func TestInvoiceItemsListSkipsRegularSubscriptionLines(t *testing.T) {
	handler := newTestHandler(t)
	customer, lite, _, _ := setupProrationPlans(t, handler)
	postForm[prorationSubResponse](t, handler, "/v1/subscriptions", url.Values{"customer": {customer.ID}, "items[0][price]": {lite.ID}})
	list := getJSON[struct {
		Data []map[string]any `json:"data"`
	}](t, handler, "/v1/invoiceitems?customer="+customer.ID)
	if len(list.Data) != 0 {
		t.Fatalf("invoiceitems=%#v, want regular subscription lines excluded", list.Data)
	}
}
