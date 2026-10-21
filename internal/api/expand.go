package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type bufferedResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

// serveExpanded routes the request and, when it carries expand[] paths,
// replaces the ids found at those paths with the objects they reference.
func (h *Handler) serveExpanded(w http.ResponseWriter, r *http.Request) {
	paths := expandPaths(r)
	if len(paths) == 0 {
		h.mux.ServeHTTP(w, r)
		return
	}
	buffered := &bufferedResponseWriter{header: w.Header()}
	h.mux.ServeHTTP(buffered, r)
	body := buffered.body.Bytes()
	if buffered.status == http.StatusOK {
		var decoded any
		if err := json.Unmarshal(body, &decoded); err == nil {
			for _, path := range paths {
				decoded = h.expandPath(r, decoded, strings.Split(path, "."))
			}
			if encoded, err := json.Marshal(decoded); err == nil {
				body = encoded
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			}
		}
	}
	w.WriteHeader(buffered.status)
	_, _ = w.Write(body)
}

func expandPaths(r *http.Request) []string {
	if err := r.ParseForm(); err != nil {
		return nil
	}
	var paths []string
	for key, values := range r.Form {
		if key == "expand" || key == "expand[]" || (strings.HasPrefix(key, "expand[") && strings.HasSuffix(key, "]")) {
			paths = append(paths, values...)
		}
	}
	return paths
}

func (h *Handler) expandPath(r *http.Request, value any, path []string) any {
	switch current := value.(type) {
	case []any:
		for index, item := range current {
			current[index] = h.expandPath(r, item, path)
		}
		return current
	case map[string]any:
		if len(path) == 0 {
			return current
		}
		if field, ok := current[path[0]]; ok {
			current[path[0]] = h.expandPath(r, field, path[1:])
		}
		return current
	case string:
		object, ok := h.expandableObject(r, current)
		if !ok {
			return current
		}
		return h.expandPath(r, object, path)
	default:
		return value
	}
}

func (h *Handler) expandableObject(r *http.Request, id string) (any, bool) {
	ctx := r.Context()
	var object map[string]any
	switch {
	case strings.HasPrefix(id, "prod_"):
		product, err := h.billing.GetProduct(ctx, id)
		if err != nil {
			return nil, false
		}
		object = stripeProduct(product)
	case strings.HasPrefix(id, "price_"):
		price, err := h.billing.GetPrice(ctx, id)
		if err != nil {
			return nil, false
		}
		object = stripePrice(price)
	case strings.HasPrefix(id, "cus_"):
		customer, err := h.billing.GetCustomer(ctx, id)
		if err != nil {
			return nil, false
		}
		object = stripeCustomer(customer)
	case strings.HasPrefix(id, "sub_"):
		subscription, err := h.billing.GetSubscription(ctx, id)
		if err != nil {
			return nil, false
		}
		object = h.stripeSubscription(r, subscription)
	case strings.HasPrefix(id, "in_"):
		invoice, err := h.billing.GetInvoice(ctx, id)
		if err != nil {
			return nil, false
		}
		object = h.stripeInvoice(ctx, invoice)
	case strings.HasPrefix(id, "pi_"):
		intent, err := h.billing.GetPaymentIntent(ctx, id)
		if err != nil {
			return nil, false
		}
		object = stripePaymentIntent(intent)
	case strings.HasPrefix(id, "cs_"):
		session, err := h.billing.GetCheckoutSession(ctx, id)
		if err != nil {
			return nil, false
		}
		object = h.stripeCheckoutSession(r, session)
	default:
		return nil, false
	}
	var generic any
	encoded, err := json.Marshal(object)
	if err != nil || json.Unmarshal(encoded, &generic) != nil {
		return nil, false
	}
	return generic, true
}
