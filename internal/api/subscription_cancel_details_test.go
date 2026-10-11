package api

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/midagedev/billtap/internal/billing"
)

func TestDeleteSubscriptionStoresCancellationDetails(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"cancel-details@example.test"}})
	product := postForm[map[string]any](t, handler, "/v1/products", url.Values{"name": {"Plan"}})
	price := postForm[map[string]any](t, handler, "/v1/prices", url.Values{"product": {fmt.Sprint(product["id"])}, "currency": {"usd"}, "unit_amount": {"1000"}, "recurring[interval]": {"month"}})
	sub := postForm[map[string]any](t, handler, "/v1/subscriptions", url.Values{"customer": {customer.ID}, "items[0][price]": {fmt.Sprint(price["id"])}})

	query := url.Values{"cancellation_details[feedback]": {"too_expensive"}, "cancellation_details[comment]": {"bye"}}
	canceled := deleteJSON[map[string]any](t, handler, "/v1/subscriptions/"+fmt.Sprint(sub["id"])+"?"+query.Encode())
	details, _ := canceled["cancellation_details"].(map[string]any)
	if canceled["status"] != "canceled" || details["feedback"] != "too_expensive" || details["comment"] != "bye" {
		t.Fatalf("status=%v cancellation_details=%#v", canceled["status"], canceled["cancellation_details"])
	}
}
