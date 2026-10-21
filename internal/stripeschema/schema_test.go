package stripeschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseReportsViolationsAgainstStripeSchemas(t *testing.T) {
	validator, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   []string
	}{
		{name: "valid customer list", status: 200, body: `{"object":"list","data":[{"id":"cus_1","object":"customer","created":1,"livemode":false,"email":null,"metadata":{"plan":"pro"},"emulator_note":"ok"}],"has_more":false,"url":"/v1/customers"}`},
		{name: "unknown field", status: 200, body: `{"id":"cus_1","object":"customer","created":1,"livemode":false,"created_at":"2026"}`, want: []string{"$.created_at: field is not in the Stripe customer schema"}},
		{name: "null and missing", status: 200, body: `{"id":"cus_1","object":"customer","livemode":null}`, want: []string{"$.created: required customer field is missing", "$.livemode: null is not allowed"}},
		{name: "expanded object judged as object", status: 200, body: `{"id":"price_1","object":"price","active":true,"billing_scheme":"per_unit","created":1,"currency":"usd","livemode":false,"metadata":{},"product":{"id":"prod_1","object":"product","active":true,"created":1,"images":[],"livemode":false,"marketing_features":[],"metadata":{},"name":"Plan","updated":1,"bogus":1},"type":"one_time"}`, want: []string{"$.product.bogus: field is not in the Stripe product schema"}},
		{name: "event payload object", status: 200, body: `{"id":"evt_1","object":"event","api_version":"2025-12-15.clover","created":1,"livemode":false,"pending_webhooks":0,"request":{"id":null,"idempotency_key":null},"type":"customer.created","data":{"object":{"id":"cus_1","object":"customer","created":1,"livemode":"no"}}}`, want: []string{"$.data.object.livemode: string is not a boolean"}},
		{name: "error envelope", status: 404, body: `{"error":{"type":"invalid_request_error","message":"No such customer: 'cus_1'","code":"resource_missing","param":"id","extra":true}}`, want: []string{"$.error.extra: field is not in the Stripe api_errors schema"}},
		{name: "unknown object", status: 200, body: `{"object":"portal_action"}`, want: []string{`$: unknown object "portal_action"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body any
			if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := validator.Response(tc.status, body)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("violations =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
