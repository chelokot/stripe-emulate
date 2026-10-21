package api

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

func TestInvoiceItemTaxRatesTaxTheInvoice(t *testing.T) {
	handler := withStripeVersion(newTestHandler(t), legacyStripeVersion)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"ii-tax@example.test"}})
	product := postForm[map[string]any](t, handler, "/v1/products", url.Values{"name": {"Extra export"}})
	price := postForm[map[string]any](t, handler, "/v1/prices", url.Values{"product": {fmt.Sprint(product["id"])}, "currency": {"usd"}, "unit_amount": {"10000"}})
	vat := postForm[map[string]any](t, handler, "/v1/tax_rates", url.Values{"display_name": {"VAT"}, "percentage": {"10"}, "inclusive": {"false"}})
	other := postForm[map[string]any](t, handler, "/v1/tax_rates", url.Values{"display_name": {"Other"}, "percentage": {"5"}, "inclusive": {"false"}})

	invoice := postForm[map[string]any](t, handler, "/v1/invoices", url.Values{"customer": {customer.ID}, "currency": {"usd"}})
	invoiceID := fmt.Sprint(invoice["id"])
	item := postForm[map[string]any](t, handler, "/v1/invoiceitems", url.Values{
		"customer":       {customer.ID},
		"invoice":        {invoiceID},
		"pricing[price]": {fmt.Sprint(price["id"])},
		"tax_rates[0]":   {fmt.Sprint(vat["id"])},
	})
	if rates, _ := item["tax_rates"].([]any); len(rates) != 1 {
		t.Fatalf("invoice item tax_rates=%#v, want the VAT rate", item["tax_rates"])
	}

	// A second line with a different rate set would need per-line tax math.
	if status, body := postFormStatus(t, handler, "/v1/invoiceitems", url.Values{
		"customer": {customer.ID}, "invoice": {invoiceID}, "amount": {"100"}, "currency": {"usd"}, "tax_rates[0]": {fmt.Sprint(other["id"])},
	}); status != 400 || !strings.Contains(body, "tax_rates") {
		t.Fatalf("mixed rates status=%d body=%s, want 400", status, body)
	}

	postForm[map[string]any](t, handler, "/v1/invoices/"+invoiceID+"/finalize", url.Values{})
	paid := postForm[map[string]any](t, handler, "/v1/invoices/"+invoiceID+"/pay", url.Values{"expand[]": {"payments"}})
	for field, want := range map[string]int64{"subtotal": 10000, "tax": 1000, "total": 11000, "amount_paid": 11000} {
		if got, _ := paid[field].(float64); int64(got) != want {
			t.Fatalf("paid invoice %s=%v, want %d (status=%v)", field, paid[field], want, paid["status"])
		}
	}
}

func TestChargeLookupFromPaymentIntent(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"charge@example.test"}})
	invoice := postForm[map[string]any](t, handler, "/v1/invoices", url.Values{"customer": {customer.ID}, "currency": {"usd"}})
	invoiceID := fmt.Sprint(invoice["id"])
	postForm[map[string]any](t, handler, "/v1/invoiceitems", url.Values{"customer": {customer.ID}, "invoice": {invoiceID}, "amount": {"10000"}, "currency": {"usd"}})
	postForm[map[string]any](t, handler, "/v1/invoices/"+invoiceID+"/finalize", url.Values{})
	paid := postForm[map[string]any](t, handler, "/v1/invoices/"+invoiceID+"/pay", url.Values{"expand[]": {"payments"}})

	payments, _ := paid["payments"].(map[string]any)
	data, _ := payments["data"].([]any)
	payment, _ := data[0].(map[string]any)["payment"].(map[string]any)
	if payment["type"] != "payment_intent" {
		t.Fatalf("payment=%#v, want type payment_intent", payment)
	}
	paymentIntent, _ := payment["payment_intent"].(string)
	piID := paymentIntent
	pi := getJSON[map[string]any](t, handler, "/v1/payment_intents/"+piID)
	chargeID, _ := pi["latest_charge"].(string)
	if chargeID == "" {
		t.Fatalf("pi latest_charge missing: %#v", pi["latest_charge"])
	}
	charge := getJSON[map[string]any](t, handler, "/v1/charges/"+chargeID+"?expand[]=balance_transaction")
	txn, _ := charge["balance_transaction"].(map[string]any)
	if charge["payment_intent"] != piID || txn["fee"] != float64(320) || txn["net"] != float64(9680) {
		t.Fatalf("charge=%#v", charge)
	}
	if status, _ := postFormStatus(t, handler, "/v1/refunds", url.Values{"charge": {chargeID}, "amount": {"2500"}}); status != 200 {
		t.Fatalf("refund by charge status=%d", status)
	}
	if refunded := getJSON[map[string]any](t, handler, "/v1/charges/"+chargeID); refunded["amount_refunded"] != float64(2500) {
		t.Fatalf("amount_refunded=%v, want 2500", refunded["amount_refunded"])
	}
	if status, _ := getStatus(t, handler, "/v1/charges/ch_unknown"); status != 404 {
		t.Fatalf("unknown charge status=%d, want 404", status)
	}
}
