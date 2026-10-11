package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/midagedev/billtap/internal/billing"
	"github.com/midagedev/billtap/internal/webhooks"
)

func TestCheckoutPaymentModeInvoiceCreation(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"invoice-creation@example.test"}})
	product := postForm[map[string]any](t, handler, "/v1/products", url.Values{"name": {"Extra export"}})
	price := postForm[map[string]any](t, handler, "/v1/prices", url.Values{
		"product":     {fmt.Sprint(product["id"])},
		"currency":    {"usd"},
		"unit_amount": {"4900"},
	})

	session := postForm[map[string]any](t, handler, "/v1/checkout/sessions", url.Values{
		"customer":                {customer.ID},
		"mode":                    {"payment"},
		"client_reference_id":     {"ref_ex"},
		"line_items[0][price]":    {fmt.Sprint(price["id"])},
		"line_items[0][quantity]": {"2"},
		"payment_intent_data[setup_future_usage]":          {"off_session"},
		"payment_intent_data[metadata][paymentType]":       {"EXTRA_EXPORT"},
		"metadata[paymentType]":                            {"EXTRA_EXPORT"},
		"invoice_creation[enabled]":                        {"true"},
		"invoice_creation[invoice_data][metadata][source]": {"checkout"},
		"invoice_creation[invoice_data][description]":      {"accepted, not modeled"},
		"success_url": {"http://app.test/success"},
	})
	ic, _ := session["invoice_creation"].(map[string]any)
	if ic["enabled"] != true {
		t.Fatalf("session invoice_creation=%#v, want enabled", session["invoice_creation"])
	}

	completion := postJSON[map[string]json.RawMessage](t, handler, "/api/checkout/sessions/"+fmt.Sprint(session["id"])+"/complete", map[string]string{"outcome": "payment_succeeded"})
	var completed billing.CheckoutSession
	if err := json.Unmarshal(completion["session"], &completed); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if completed.InvoiceID == "" || completed.PaymentIntentID == "" || completed.SubscriptionID != "" {
		t.Fatalf("completed=%#v, want invoice + payment_intent, no subscription", completed)
	}

	var events []webhooks.Event
	if err := json.Unmarshal(completion["webhook_events"], &events); err != nil {
		t.Fatalf("decode webhook events: %v", err)
	}
	var types []string
	for _, event := range events {
		types = append(types, event.Type)
	}
	want := []string{"checkout.session.completed", "invoice.created", "invoice.finalized", "payment_intent.created", "payment_intent.succeeded", "invoice.payment_succeeded", "invoice.paid"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("webhook order=%v, want %v", types, want)
	}

	list := getJSON[struct {
		Data []map[string]any `json:"data"`
	}](t, handler, "/v1/invoices?customer="+customer.ID+"&status=paid&expand[]=data.payments.data.payment")
	if len(list.Data) != 1 {
		t.Fatalf("paid invoices=%d, want 1", len(list.Data))
	}
	inv := list.Data[0]
	if inv["id"] != completed.InvoiceID || inv["billing_reason"] != "manual" || inv["subscription"] != nil {
		t.Fatalf("invoice id=%v billing_reason=%v subscription=%v", inv["id"], inv["billing_reason"], inv["subscription"])
	}
	if amount, _ := inv["amount_paid"].(float64); int64(amount) != 9800 {
		t.Fatalf("amount_paid=%v, want 9800", inv["amount_paid"])
	}
	if transitions, _ := inv["status_transitions"].(map[string]any); transitions["paid_at"] == nil {
		t.Fatalf("status_transitions.paid_at missing: %#v", inv["status_transitions"])
	}
	if inv["hosted_invoice_url"] == nil {
		t.Fatalf("hosted_invoice_url missing")
	}
	meta, _ := inv["metadata"].(map[string]any)
	if meta["source"] != "checkout" || meta["paymentType"] != nil || len(meta) != 1 {
		t.Fatalf("invoice metadata=%#v, want only invoice_data.metadata", meta)
	}
	lines, _ := inv["lines"].(map[string]any)
	lineData, _ := lines["data"].([]any)
	if len(lineData) != 1 {
		t.Fatalf("invoice lines=%#v, want 1", lines)
	}
	payments, _ := inv["payments"].(map[string]any)
	paymentData, _ := payments["data"].([]any)
	if len(paymentData) != 1 {
		t.Fatalf("invoice payments=%#v, want 1", payments)
	}
	payment, _ := paymentData[0].(map[string]any)["payment"].(map[string]any)
	piID := payment["payment_intent"]
	if obj, ok := piID.(map[string]any); ok {
		piID = obj["id"]
	}
	if piID != completed.PaymentIntentID {
		t.Fatalf("payments.data[0].payment.payment_intent=%v, want %s", piID, completed.PaymentIntentID)
	}

	pi := getJSON[map[string]any](t, handler, "/v1/payment_intents/"+completed.PaymentIntentID)
	if piMeta, _ := pi["metadata"].(map[string]any); piMeta["paymentType"] != "EXTRA_EXPORT" {
		t.Fatalf("pi metadata=%#v, want payment_intent_data.metadata", pi["metadata"])
	}
}

func TestCheckoutInvoiceCreationValidation(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"invoice-creation-bad@example.test"}})
	base := func(extra url.Values) url.Values {
		v := url.Values{
			"customer":                               {customer.ID},
			"mode":                                   {"payment"},
			"line_items[0][price_data][currency]":    {"usd"},
			"line_items[0][price_data][unit_amount]": {"100"},
			"line_items[0][price_data][product_data][name]": {"x"},
		}
		for k, vals := range extra {
			v[k] = vals
		}
		return v
	}
	for name, extra := range map[string]url.Values{
		"not a bool":        {"invoice_creation[enabled]": {"yes please"}},
		"data without flag": {"invoice_creation[invoice_data][metadata][a]": {"b"}},
		"unknown sub key":   {"invoice_creation[enabled]": {"true"}, "invoice_creation[bogus]": {"1"}},
		"subscription mode": {"invoice_creation[enabled]": {"true"}, "mode": {"subscription"}},
	} {
		if status, body := postFormStatus(t, handler, "/v1/checkout/sessions", base(extra)); status != http.StatusBadRequest || !strings.Contains(body, "invoice_creation") {
			t.Fatalf("%s: status=%d body=%s, want 400 naming invoice_creation", name, status, body)
		}
	}
}
