package api

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestCheckoutSubscriptionDataMetadataBecomesSubscriptionMetadata(t *testing.T) {
	handler := newTestHandler(t)
	customer, lite, _, _ := setupProrationPlans(t, handler)
	session := postForm[struct {
		ID string `json:"id"`
	}](t, handler, "/v1/checkout/sessions", url.Values{
		"customer":                {customer.ID},
		"mode":                    {"subscription"},
		"line_items[0][price]":    {lite.ID},
		"line_items[0][quantity]": {"1"},
		"line_items[0][adjustable_quantity][enabled]": {"true"},
		"line_items[0][adjustable_quantity][minimum]": {"1"},
		"line_items[0][adjustable_quantity][maximum]": {"10"},
		"subscription_data[metadata][workspace_id]":   {"ws_123"},
		"success_url": {"https://example.com/success"},
	})

	lineItems := getJSON[struct {
		Data []struct {
			AdjustableQuantity map[string]any `json:"adjustable_quantity"`
		} `json:"data"`
	}](t, handler, "/v1/checkout/sessions/"+session.ID+"/line_items")
	if want := map[string]any{"enabled": true, "minimum": float64(1), "maximum": float64(10)}; len(lineItems.Data) != 1 || !reflect.DeepEqual(lineItems.Data[0].AdjustableQuantity, want) {
		t.Fatalf("line item adjustable_quantity = %#v, want %#v", lineItems.Data, want)
	}

	completion := postJSON[map[string]json.RawMessage](t, handler, "/api/checkout/sessions/"+session.ID+"/complete", map[string]string{"outcome": "payment_succeeded"})
	var completed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(completion["subscription"], &completed); err != nil {
		t.Fatalf("decode completed subscription: %v", err)
	}
	subscription := getJSON[struct {
		Metadata map[string]string `json:"metadata"`
	}](t, handler, "/v1/subscriptions/"+completed.ID)
	if want := map[string]string{"workspace_id": "ws_123"}; !reflect.DeepEqual(subscription.Metadata, want) {
		t.Fatalf("subscription metadata = %#v, want exactly %#v", subscription.Metadata, want)
	}
}

func TestSubscriptionCreateEmitsCreatedAndFirstInvoiceEvents(t *testing.T) {
	handler := newTestHandler(t)
	customer, lite, _, _ := setupProrationPlans(t, handler)
	subscription := postForm[struct {
		ID            string `json:"id"`
		LatestInvoice string `json:"latest_invoice"`
	}](t, handler, "/v1/subscriptions", url.Values{
		"customer":        {customer.ID},
		"items[0][price]": {lite.ID},
	})

	events := getJSON[struct {
		Data []struct {
			Type string `json:"type"`
			Data struct {
				Object struct {
					ID string `json:"id"`
				} `json:"object"`
			} `json:"data"`
		} `json:"data"`
	}](t, handler, "/v1/events")
	seen := map[string]string{}
	for _, event := range events.Data {
		seen[event.Type] = event.Data.Object.ID
	}
	for eventType, objectID := range map[string]string{
		"customer.subscription.created": subscription.ID,
		"invoice.created":               subscription.LatestInvoice,
		"invoice.paid":                  subscription.LatestInvoice,
	} {
		if seen[eventType] != objectID {
			t.Fatalf("%s object = %q, want %q; events = %#v", eventType, seen[eventType], objectID, seen)
		}
	}
}
