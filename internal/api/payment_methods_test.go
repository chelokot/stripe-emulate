package api

import (
	"net/url"
	"strings"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

func TestMintedPaymentMethodKeepsTokenBehavior(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"minted@example.test"}})
	type paymentMethod struct {
		ID   string `json:"id"`
		Card struct {
			Brand string `json:"brand"`
			Last4 string `json:"last4"`
		} `json:"card"`
	}

	mastercard := postForm[paymentMethod](t, handler, "/v1/payment_methods/pm_card_mastercard/attach", url.Values{"customer": {customer.ID}})
	again := postForm[paymentMethod](t, handler, "/v1/payment_methods/pm_card_mastercard/attach", url.Values{"customer": {customer.ID}})
	if !strings.HasPrefix(mastercard.ID, "pm_") || mastercard.ID == "pm_card_mastercard" || again.ID == mastercard.ID {
		t.Fatalf("attached ids = %s, %s, want a new payment method per attach", mastercard.ID, again.ID)
	}
	retrieved := getJSON[paymentMethod](t, handler, "/v1/payment_methods/"+mastercard.ID)
	if retrieved.Card.Brand != "mastercard" || retrieved.Card != mastercard.Card {
		t.Fatalf("retrieved card = %#v, want mastercard details of %#v", retrieved.Card, mastercard.Card)
	}

	declining := postForm[paymentMethod](t, handler, "/v1/payment_methods/pm_card_visa_chargeDeclined/attach", url.Values{"customer": {customer.ID}})
	intent := postForm[struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		PaymentMethod    string `json:"payment_method"`
		LastPaymentError *struct {
			Code string `json:"code"`
		} `json:"last_payment_error"`
	}](t, handler, "/v1/payment_intents", url.Values{
		"amount":         {"1000"},
		"currency":       {"usd"},
		"customer":       {customer.ID},
		"payment_method": {declining.ID},
		"confirm":        {"true"},
	})
	if intent.Status != "requires_payment_method" || intent.PaymentMethod != declining.ID || intent.LastPaymentError == nil || intent.LastPaymentError.Code != "card_declined" {
		t.Fatalf("intent = %#v, want card_declined on minted %s", intent, declining.ID)
	}
}
