package api

import (
	"net/url"
	"testing"
)

func TestInvoiceLegacyFieldsFollowStripeVersion(t *testing.T) {
	handler := newTestHandler(t)
	customer, lite, _, taxRateID := setupProrationPlans(t, handler)
	subscription := postForm[struct {
		ID            string `json:"id"`
		LatestInvoice string `json:"latest_invoice"`
	}](t, handler, "/v1/subscriptions", url.Values{
		"customer":             {customer.ID},
		"items[0][price]":      {lite.ID},
		"default_tax_rates[0]": {taxRateID},
	})
	path := "/v1/invoices/" + subscription.LatestInvoice

	current := getJSON[map[string]any](t, handler, path)
	for _, legacyField := range []string{"subscription", "paid", "payment_intent", "tax", "total_tax_amounts", "charge", "discount"} {
		if _, ok := current[legacyField]; ok {
			t.Fatalf("current invoice has legacy field %q: %#v", legacyField, current[legacyField])
		}
	}
	currentLine := current["lines"].(map[string]any)["data"].([]any)[0].(map[string]any)
	for _, legacyField := range []string{"price", "subscription", "proration", "type", "tax_amounts"} {
		if _, ok := currentLine[legacyField]; ok {
			t.Fatalf("current line has legacy field %q: %#v", legacyField, currentLine[legacyField])
		}
	}

	legacy := getJSON[struct {
		Subscription    string `json:"subscription"`
		Paid            bool   `json:"paid"`
		PaymentIntent   string `json:"payment_intent"`
		Tax             int64  `json:"tax"`
		TotalTaxAmounts []struct {
			Amount  int64  `json:"amount"`
			TaxRate string `json:"tax_rate"`
		} `json:"total_tax_amounts"`
		TotalTaxes []struct {
			Amount int64 `json:"amount"`
		} `json:"total_taxes"`
		Lines struct {
			Data []struct {
				Type         string `json:"type"`
				Subscription string `json:"subscription"`
				Proration    bool   `json:"proration"`
				Price        struct {
					ID     string `json:"id"`
					Object string `json:"object"`
				} `json:"price"`
			} `json:"data"`
		} `json:"lines"`
	}](t, withStripeVersion(handler, legacyStripeVersion), path)
	if legacy.Subscription != subscription.ID || !legacy.Paid || legacy.PaymentIntent == "" {
		t.Fatalf("legacy invoice = %#v, want subscription %s paid with payment intent", legacy, subscription.ID)
	}
	if len(legacy.TotalTaxes) != 1 || legacy.Tax != legacy.TotalTaxes[0].Amount || len(legacy.TotalTaxAmounts) != 1 || legacy.TotalTaxAmounts[0].TaxRate != taxRateID {
		t.Fatalf("legacy invoice taxes = %#v, want tax and total_tax_amounts derived from total_taxes", legacy)
	}
	if len(legacy.Lines.Data) != 1 {
		t.Fatalf("legacy lines = %#v, want 1", legacy.Lines.Data)
	}
	line := legacy.Lines.Data[0]
	if line.Type != "subscription" || line.Subscription != subscription.ID || line.Proration || line.Price.ID != lite.ID || line.Price.Object != "price" {
		t.Fatalf("legacy line = %#v, want subscription line with expanded price %s", line, lite.ID)
	}
}
