package api

import (
	"fmt"
	"net/url"
	"strconv"
	"testing"

	"github.com/midagedev/billtap/internal/billing"
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
