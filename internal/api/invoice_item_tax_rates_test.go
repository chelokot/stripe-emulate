package api

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/midagedev/billtap/internal/billing"
)

func TestInvoiceItemTaxRatesTaxTheInvoice(t *testing.T) {
	handler := newTestHandler(t)
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
	paid := postForm[map[string]any](t, handler, "/v1/invoices/"+invoiceID+"/pay", url.Values{})
	for field, want := range map[string]int64{"subtotal": 10000, "tax": 1000, "total": 11000, "amount_paid": 11000} {
		if got, _ := paid[field].(float64); int64(got) != want {
			t.Fatalf("paid invoice %s=%v, want %d (status=%v)", field, paid[field], want, paid["status"])
		}
	}
}
