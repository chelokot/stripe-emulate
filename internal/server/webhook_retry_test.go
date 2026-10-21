package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chelokot/stripe-emulate/internal/webhooks"
)

// A failed delivery is retried on its own after the endpoint's backoff,
// freshly signed, until the app accepts it.
func TestFailedWebhookDeliveryIsRetriedAutomatically(t *testing.T) {
	srv, _ := newRunServer(t)
	const secret = "whsec_retry_test"
	var mu sync.Mutex
	var deliveries []time.Time
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := webhooks.VerifySignature(r.Header.Get(webhooks.SignatureHeaderName), secret, time.Now(), body, 5*time.Second); err != nil {
			t.Errorf("delivery signature: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		deliveries = append(deliveries, time.Now())
		if len(deliveries) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	endpoint := postForm[struct {
		ID string `json:"id"`
	}](t, srv, "/v1/webhook_endpoints", map[string]string{
		"url":              receiver.URL,
		"secret":           secret,
		"enabled_events[]": "customer.subscription.created",
		"retry_backoff[0]": "1s",
	})
	customer := postForm[struct {
		ID string `json:"id"`
	}](t, srv, "/v1/customers", map[string]string{"email": "retry@example.test"})
	product := postForm[struct {
		ID string `json:"id"`
	}](t, srv, "/v1/products", map[string]string{"name": "Retry"})
	price := postForm[struct {
		ID string `json:"id"`
	}](t, srv, "/v1/prices", map[string]string{"product": product.ID, "currency": "usd", "unit_amount": "1000", "recurring[interval]": "month"})
	_ = postForm[map[string]any](t, srv, "/v1/subscriptions", map[string]string{"customer": customer.ID, "items[0][price]": price.ID})

	attempts := waitForRunListCount(t, srv, "/v1/webhook_endpoints/"+endpoint.ID+"/attempts?status=succeeded", 1)
	if len(attempts.Data) != 1 {
		t.Fatalf("succeeded attempts = %d, want the retry to succeed", len(attempts.Data))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deliveries) != 2 || deliveries[1].Sub(deliveries[0]) < time.Second {
		t.Fatalf("deliveries = %v, want a failure and one retry at least 1s later", deliveries)
	}
}
