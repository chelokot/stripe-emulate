package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/storage"
	"github.com/chelokot/stripe-emulate/internal/stripecompat"
	"github.com/chelokot/stripe-emulate/internal/stripeschema"
	"github.com/chelokot/stripe-emulate/internal/webhooks"
)

var stripeSchemas = sync.OnceValues(stripeschema.New)

// withStripeSchemaCheck fails the test whenever a response to a Stripe API
// route, or a webhook event recorded in store, breaks the Stripe object
// schemas for the emulated API version. Pre-basil clients are skipped: they
// deliberately get legacy fields.
func withStripeSchemaCheck(t *testing.T, handler http.Handler, store *storage.SQLiteStore) http.Handler {
	t.Helper()
	validator, err := stripeSchemas()
	if err != nil {
		t.Fatalf("load Stripe schemas: %v", err)
	}
	routes := stripecompat.DefaultRouteCatalog()
	t.Cleanup(func() {
		events, err := store.ListEvents(context.Background(), webhooks.EventFilter{})
		if err != nil {
			t.Errorf("list events for schema check: %v", err)
			return
		}
		for _, event := range events {
			var payload any
			if err := json.Unmarshal(event.RawPayload, &payload); err != nil {
				t.Errorf("schema: event %s: decode payload: %v", event.Type, err)
				continue
			}
			for _, violation := range validator.Response(http.StatusOK, payload) {
				t.Errorf("schema: event %s: %s", event.Type, violation)
			}
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, known := routes.Lookup(r.Method, r.URL.Path); !known || legacyAPIVersion(r) {
			handler.ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
		var body any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Errorf("schema: %s %s: decode response: %v", r.Method, r.URL.Path, err)
			return
		}
		for _, violation := range validator.Response(recorder.Code, body) {
			t.Errorf("schema: %s %s: %s", r.Method, r.URL.Path, violation)
		}
	})
}
