package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/storage"
	"github.com/chelokot/stripe-emulate/internal/stripecompat"
	"github.com/chelokot/stripe-emulate/internal/stripeschema"
	"github.com/chelokot/stripe-emulate/internal/webhooks"
)

var stripeSchemas = sync.OnceValues(stripeschema.New)

// schemaCoverage tallies what the suite validated; TestMain writes it to
// $STRIPE_SCHEMA_SUMMARY as Markdown when that is set.
var schemaCoverage = struct {
	sync.Mutex
	responses, events int
	schemas           map[string]bool
}{schemas: map[string]bool{}}

func observeSchema(schema string) {
	schemaCoverage.Lock()
	schemaCoverage.schemas[schema] = true
	schemaCoverage.Unlock()
}

func TestMain(m *testing.M) {
	code := m.Run()
	if path := os.Getenv("STRIPE_SCHEMA_SUMMARY"); path != "" && code == 0 {
		summary := fmt.Sprintf("| Checked against Stripe 2025-12-15.clover schemas | Count |\n| --- | ---: |\n| API responses | %d |\n| Webhook events | %d |\n| Distinct Stripe object schemas | %d |\n| Violations | 0 |\n",
			schemaCoverage.responses, schemaCoverage.events, len(schemaCoverage.schemas))
		if err := os.WriteFile(path, []byte(summary), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write schema summary:", err)
			code = 1
		}
	}
	os.Exit(code)
}

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
	validator = validator.Observing(observeSchema)
	routes := stripecompat.DefaultRouteCatalog()
	t.Cleanup(func() {
		events, err := store.ListEvents(context.Background(), webhooks.EventFilter{})
		if err != nil {
			t.Errorf("list events for schema check: %v", err)
			return
		}
		schemaCoverage.Lock()
		schemaCoverage.events += len(events)
		schemaCoverage.Unlock()
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
		schemaCoverage.Lock()
		schemaCoverage.responses++
		schemaCoverage.Unlock()
		for _, violation := range validator.Response(recorder.Code, body) {
			t.Errorf("schema: %s %s: %s", r.Method, r.URL.Path, violation)
		}
	})
}
