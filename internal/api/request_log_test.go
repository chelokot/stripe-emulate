package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

func TestUnknownParamsReportedDeterministically(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"unknown@example.test"}})
	values := url.Values{
		"customer":               {customer.ID},
		"mode":                   {"payment"},
		"line_items[0][price]":   {"price_x"},
		"zzz_last":               {"1"},
		"aaa_first":              {"1"},
		"line_items[0][mystery]": {"1"},
	}
	for i := 0; i < 20; i++ {
		status, body := postFormStatus(t, handler, "/v1/checkout/sessions", values)
		var payload struct {
			Error stripeAPIError `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil || status != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", status, body)
		}
		if payload.Error.Code != stripeCodeParamUnknown || payload.Error.Param != "aaa_first" {
			t.Fatalf("error=%#v, want parameter_unknown param=aaa_first", payload.Error)
		}
		if want := "Received unknown parameters: aaa_first, line_items[0][mystery], zzz_last"; payload.Error.Message != want {
			t.Fatalf("message=%q, want %q", payload.Error.Message, want)
		}
	}
}

func TestRequestLogLine(t *testing.T) {
	var buf bytes.Buffer
	handler := newTestHandlerWithOptions(t, Options{RequestLog: slog.New(slog.NewTextHandler(&buf, nil))})
	postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"log@example.test"}})
	if line := buf.String(); !strings.Contains(line, "method=POST path=/v1/customers") || !strings.Contains(line, "status=200") || !strings.Contains(line, "request_id=") || strings.Contains(line, "param_keys") {
		t.Fatalf("2xx log line=%q", line)
	}
	buf.Reset()
	postFormStatus(t, handler, "/v1/customers", url.Values{"email": {"secret-value@example.test"}, "bogus": {"x"}})
	line := buf.String()
	for _, want := range []string{"status=400", "error_code=parameter_unknown", "error_param=bogus", "param_keys=\"[bogus email]\""} {
		if !strings.Contains(line, want) {
			t.Fatalf("4xx log line missing %q: %q", want, line)
		}
	}
	buf.Reset()
	postFormStatus(t, handler, "/v1/customers", url.Values{"email": {"secret-value@example.test"}, "card[number]": {"4242424242424242"}})
	line += buf.String()
	if !strings.Contains(buf.String(), "card[number]") || strings.Contains(line, "4242424242424242") || strings.Contains(line, "secret-value") {
		t.Fatalf("log lines must name keys but never values: %q", line)
	}
}
