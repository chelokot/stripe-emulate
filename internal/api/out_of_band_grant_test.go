package api

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/midagedev/billtap/internal/billing"
)

// Stripe accepts amount=0 invoice items (e.g. a 0-cost recurring price).
func TestZeroAmountInvoiceItem(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"zero-item@example.test"}})
	byAmount := postForm[map[string]any](t, handler, "/v1/invoiceitems", url.Values{"customer": {customer.ID}, "amount": {"0"}, "currency": {"usd"}})
	if byAmount["amount"] != float64(0) {
		t.Fatalf("amount item=%#v, want amount 0", byAmount)
	}
	product := postForm[map[string]any](t, handler, "/v1/products", url.Values{"name": {"Granted plan"}})
	free := postForm[map[string]any](t, handler, "/v1/prices", url.Values{"product": {fmt.Sprint(product["id"])}, "currency": {"usd"}, "unit_amount": {"0"}, "recurring[interval]": {"month"}})
	byPrice := postForm[map[string]any](t, handler, "/v1/invoiceitems", url.Values{"customer": {customer.ID}, "pricing[price]": {fmt.Sprint(free["id"])}})
	if byPrice["amount"] != float64(0) {
		t.Fatalf("price item=%#v, want amount 0", byPrice)
	}
}

// Paying a draft subscription invoice made through POST /v1/invoices finalizes
// it first (Stripe behavior), including paid_out_of_band.
func TestPayDraftSubscriptionInvoiceFinalizes(t *testing.T) {
	handler := newTestHandler(t)
	customer, lite, _, _ := setupProrationPlans(t, handler)
	sub := postForm[prorationSubResponse](t, handler, "/v1/subscriptions", url.Values{"customer": {customer.ID}, "items[0][price]": {lite.ID}})
	draft := postForm[map[string]any](t, handler, "/v1/invoices", url.Values{"customer": {customer.ID}, "subscription": {sub.ID}, "currency": {"usd"}})
	invoiceID := fmt.Sprint(draft["id"])
	if draft["status"] != "draft" {
		t.Fatalf("invoice status=%v, want draft", draft["status"])
	}
	postForm[map[string]any](t, handler, "/v1/invoiceitems", url.Values{"customer": {customer.ID}, "invoice": {invoiceID}, "amount": {"100"}, "currency": {"usd"}})
	paid := postForm[map[string]any](t, handler, "/v1/invoices/"+invoiceID+"/pay", url.Values{"paid_out_of_band": {"true"}})
	if paid["status"] != "paid" {
		t.Fatalf("paid invoice status=%v, want paid: %#v", paid["status"], paid)
	}
}
