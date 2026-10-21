package api

import (
	"net/http"
	"strings"
)

// basilVersion is the Stripe API version that removed the legacy invoice,
// invoice item and invoice line fields restored here for older clients.
const basilVersion = "2025-03-31"

func legacyAPIVersion(r *http.Request) bool {
	version := r.Header.Get("Stripe-Version")
	return version != "" && version[:min(len(version), len(basilVersion))] < basilVersion
}

func (h *Handler) addLegacyFields(r *http.Request, value any) {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			h.addLegacyFields(r, item)
		}
	case map[string]any:
		for _, field := range current {
			h.addLegacyFields(r, field)
		}
		_, isStripeInvoice := current["total_taxes"]
		_, isStripeLine := current["pricing"]
		switch {
		case current["object"] == "invoice" && isStripeInvoice:
			h.addLegacyInvoiceFields(r, current)
		case current["object"] == "line_item" && isStripeLine:
			h.addLegacyLineFields(r, current)
		case current["object"] == "invoiceitem" && isStripeLine:
			h.addLegacyInvoiceItemFields(r, current)
		}
	}
}

func (h *Handler) addLegacyInvoiceFields(r *http.Request, invoice map[string]any) {
	parent, _ := invoice["parent"].(map[string]any)
	subscriptionDetails, _ := parent["subscription_details"].(map[string]any)
	invoice["subscription"] = subscriptionDetails["subscription"]
	invoice["paid"] = invoice["status"] == "paid"
	invoice["paid_out_of_band"] = false
	invoice["charge"] = nil
	invoice["application_fee_amount"] = nil
	invoice["quote"] = nil
	invoice["transfer_data"] = nil
	invoice["rendering_options"] = nil
	discounts, _ := invoice["discounts"].([]any)
	invoice["discount"] = nil
	if len(discounts) > 0 {
		invoice["discount"] = discounts[0]
	}
	totalTaxes, _ := invoice["total_taxes"].([]any)
	totalTaxAmounts := make([]any, 0, len(totalTaxes))
	var tax any
	for _, entry := range totalTaxes {
		taxEntry, _ := entry.(map[string]any)
		details, _ := taxEntry["tax_rate_details"].(map[string]any)
		amount, _ := taxEntry["amount"].(float64)
		sum, _ := tax.(float64)
		tax = sum + amount
		totalTaxAmounts = append(totalTaxAmounts, map[string]any{
			"amount":            taxEntry["amount"],
			"inclusive":         taxEntry["tax_behavior"] == "inclusive",
			"tax_rate":          details["tax_rate"],
			"taxability_reason": taxEntry["taxability_reason"],
		})
	}
	invoice["tax"] = tax
	invoice["total_tax_amounts"] = totalTaxAmounts
	invoice["payment_intent"] = nil
	id, _ := invoice["id"].(string)
	if stored, err := h.billing.GetInvoice(r.Context(), id); err == nil && stored.PaymentIntentID != "" {
		invoice["payment_intent"] = stored.PaymentIntentID
		invoice["payments"] = stripeList("/v1/invoices/"+id+"/payments", h.stripeInvoicePayments(r.Context(), stored, nil))
	}
}

func (h *Handler) addLegacyLineFields(r *http.Request, line map[string]any) {
	parent, _ := line["parent"].(map[string]any)
	subscriptionItemDetails, isSubscriptionLine := parent["subscription_item_details"].(map[string]any)
	invoiceItemDetails, _ := parent["invoice_item_details"].(map[string]any)
	line["type"] = "invoiceitem"
	if isSubscriptionLine {
		line["subscription"] = subscriptionItemDetails["subscription"]
		line["subscription_item"] = subscriptionItemDetails["subscription_item"]
		line["proration"] = subscriptionItemDetails["proration"]
		line["invoice_item"] = subscriptionItemDetails["invoice_item"]
		if subscriptionItemDetails["invoice_item"] == nil {
			line["type"] = "subscription"
		}
	} else {
		line["subscription"] = invoiceItemDetails["subscription"]
		line["subscription_item"] = nil
		line["proration"] = invoiceItemDetails["proration"]
		line["invoice_item"] = invoiceItemDetails["invoice_item"]
	}
	line["tax_rates"] = []any{}
	taxes, _ := line["taxes"].([]any)
	taxAmounts := make([]any, 0, len(taxes))
	for _, entry := range taxes {
		taxEntry, _ := entry.(map[string]any)
		details, _ := taxEntry["tax_rate_details"].(map[string]any)
		taxAmounts = append(taxAmounts, map[string]any{
			"amount":            taxEntry["amount"],
			"inclusive":         taxEntry["tax_behavior"] == "inclusive",
			"tax_rate":          details["tax_rate"],
			"taxability_reason": taxEntry["taxability_reason"],
			"taxable_amount":    taxEntry["taxable_amount"],
		})
	}
	line["tax_amounts"] = taxAmounts
	line["price"] = h.legacyPrice(r, line["pricing"])
}

func (h *Handler) addLegacyInvoiceItemFields(r *http.Request, item map[string]any) {
	parent, _ := item["parent"].(map[string]any)
	subscriptionDetails, _ := parent["subscription_details"].(map[string]any)
	item["subscription"] = subscriptionDetails["subscription"]
	item["subscription_item"] = subscriptionDetails["subscription_item"]
	item["price"] = h.legacyPrice(r, item["pricing"])
}

func (h *Handler) legacyPrice(r *http.Request, pricing any) any {
	pricingDetails, _ := pricing.(map[string]any)
	priceDetails, _ := pricingDetails["price_details"].(map[string]any)
	priceID, _ := priceDetails["price"].(string)
	if !strings.HasPrefix(priceID, "price_") {
		return nil
	}
	price, ok := h.expandableObject(r, priceID)
	if !ok {
		return nil
	}
	return price
}
