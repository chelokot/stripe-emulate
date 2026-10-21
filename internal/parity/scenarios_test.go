package parity

import "net/http"

type step struct {
	name   string
	method string
	path   string
	form   [][2]string
}

type scenario struct {
	name  string
	steps []step
}

func request(name string, method string, path string, pairs ...string) step {
	form := make([][2]string, 0, len(pairs)/2)
	for index := 0; index+1 < len(pairs); index += 2 {
		form = append(form, [2]string{pairs[index], pairs[index+1]})
	}
	return step{name: name, method: method, path: path, form: form}
}

var scenarios = []scenario{
	{name: "customers", steps: []step{
		request("create", http.MethodPost, "/v1/customers",
			"email", "parity-{run}@example.com", "name", "Parity Customer", "description", "parity run",
			"phone", "+15555550100", "metadata[stripe_emulate_parity]", "1", "metadata[plan]", "pro"),
		request("retrieve", http.MethodGet, "/v1/customers/{create.id}"),
		request("update", http.MethodPost, "/v1/customers/{create.id}",
			"name", "Renamed Customer", "metadata[plan]", "", "preferred_locales[]", "en"),
		request("list_by_email", http.MethodGet, "/v1/customers", "email", "parity-{run}@example.com"),
		request("delete", http.MethodDelete, "/v1/customers/{create.id}"),
		request("retrieve_deleted", http.MethodGet, "/v1/customers/{create.id}"),
		request("retrieve_missing", http.MethodGet, "/v1/customers/cus_parityMissing000"),
		request("create_unknown_param", http.MethodPost, "/v1/customers", "unknown_field", "x"),
		request("create_invalid_email", http.MethodPost, "/v1/customers", "email", "not-an-email"),
	}},
	{name: "catalog_errors", steps: []step{
		request("product_missing", http.MethodGet, "/v1/products/prod_parityMissing000"),
		request("price_missing", http.MethodGet, "/v1/prices/price_parityMissing000"),
	}},
	{name: "checkout_sessions", steps: []step{
		request("customer", http.MethodPost, "/v1/customers", "email", "checkout-{run}@example.com", "metadata[stripe_emulate_parity]", "1"),
		request("subscription_session", http.MethodPost, "/v1/checkout/sessions",
			"mode", "subscription", "customer", "{customer.id}", "line_items[0][price_data][currency]", "usd", "line_items[0][price_data][unit_amount]", "2000",
			"line_items[0][price_data][recurring][interval]", "month", "line_items[0][price_data][product_data][name]", "Parity Plan {run}",
			"line_items[0][quantity]", "2",
			"success_url", "https://example.com/success", "cancel_url", "https://example.com/cancel", "metadata[stripe_emulate_parity]", "1"),
		request("payment_session", http.MethodPost, "/v1/checkout/sessions",
			"mode", "payment", "customer", "{customer.id}",
			"line_items[0][price_data][currency]", "usd", "line_items[0][price_data][unit_amount]", "2500",
			"line_items[0][price_data][product_data][name]", "Parity One-off", "line_items[0][quantity]", "1",
			"success_url", "https://example.com/success"),
		request("retrieve", http.MethodGet, "/v1/checkout/sessions/{subscription_session.id}"),
		request("line_items", http.MethodGet, "/v1/checkout/sessions/{subscription_session.id}/line_items"),
		request("line_items_expanded", http.MethodGet, "/v1/checkout/sessions/{subscription_session.id}/line_items", "expand[]", "data.price.product"),
		request("payment_line_items_expanded", http.MethodGet, "/v1/checkout/sessions/{payment_session.id}/line_items", "expand[]", "data.price.product"),
		request("list_by_customer", http.MethodGet, "/v1/checkout/sessions", "customer", "{customer.id}"),
		request("expire", http.MethodPost, "/v1/checkout/sessions/{subscription_session.id}/expire"),
		request("expire_again", http.MethodPost, "/v1/checkout/sessions/{subscription_session.id}/expire"),
		request("expire_payment", http.MethodPost, "/v1/checkout/sessions/{payment_session.id}/expire"),
		request("create_without_line_items", http.MethodPost, "/v1/checkout/sessions", "mode", "payment", "success_url", "https://example.com/success"),
		request("retrieve_missing", http.MethodGet, "/v1/checkout/sessions/cs_test_parityMissing000"),
	}},
	{name: "billing_portal", steps: []step{
		request("customer", http.MethodPost, "/v1/customers", "email", "portal-{run}@example.com", "metadata[stripe_emulate_parity]", "1"),
		request("session", http.MethodPost, "/v1/billing_portal/sessions", "customer", "{customer.id}", "return_url", "https://example.com/account"),
		request("session_missing_customer", http.MethodPost, "/v1/billing_portal/sessions", "return_url", "https://example.com/account"),
	}},
	{name: "payment_methods", steps: []step{
		request("customer", http.MethodPost, "/v1/customers", "email", "pm-{run}@example.com", "metadata[stripe_emulate_parity]", "1"),
		request("attach", http.MethodPost, "/v1/payment_methods/pm_card_visa/attach", "customer", "{customer.id}"),
		request("list", http.MethodGet, "/v1/payment_methods", "customer", "{customer.id}", "type", "card"),
		request("customer_list", http.MethodGet, "/v1/customers/{customer.id}/payment_methods"),
		request("set_default", http.MethodPost, "/v1/customers/{customer.id}", "invoice_settings[default_payment_method]", "{attach.id}"),
		request("detach", http.MethodPost, "/v1/payment_methods/{attach.id}/detach"),
	}},
	{name: "invoices", steps: []step{
		request("customer", http.MethodPost, "/v1/customers", "email", "invoice-{run}@example.com", "metadata[stripe_emulate_parity]", "1"),
		request("invoice_item", http.MethodPost, "/v1/invoiceitems", "customer", "{customer.id}", "amount", "700", "currency", "usd", "description", "Parity item"),
		request("invoice", http.MethodPost, "/v1/invoices", "customer", "{customer.id}", "auto_advance", "false", "pending_invoice_items_behavior", "include"),
		request("retrieve", http.MethodGet, "/v1/invoices/{invoice.id}"),
		request("list_by_customer", http.MethodGet, "/v1/invoices", "customer", "{customer.id}"),
		request("invoice_items_by_customer", http.MethodGet, "/v1/invoiceitems", "customer", "{customer.id}"),
		request("delete_draft", http.MethodDelete, "/v1/invoices/{invoice.id}"),
		request("retrieve_missing", http.MethodGet, "/v1/invoices/in_parityMissing000"),
	}},
}
