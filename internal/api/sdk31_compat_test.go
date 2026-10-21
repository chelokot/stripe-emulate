package api

import (
	"net/url"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

// A discount names its coupon in source.coupon and, when a promotion code
// applied it, the promotion code id beside it.
func TestDiscountSourceShape(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"sdk31@example.test"}})
	product := postForm[billing.Product](t, handler, "/v1/products", url.Values{"name": {"SDK31 Plan"}})
	price := postForm[billing.Price](t, handler, "/v1/prices", url.Values{
		"product":             {product.ID},
		"currency":            {"usd"},
		"unit_amount":         {"2000"},
		"recurring[interval]": {"month"},
	})
	coupon := postForm[struct {
		ID string `json:"id"`
	}](t, handler, "/v1/coupons", url.Values{
		"percent_off": {"10"},
		"duration":    {"forever"},
	})
	subscription := postForm[prorationSubResponse](t, handler, "/v1/subscriptions", url.Values{
		"customer":             {customer.ID},
		"items[0][price]":      {price.ID},
		"items[0][quantity]":   {"1"},
		"discounts[0][coupon]": {coupon.ID},
	})

	type discountShape struct {
		Source struct {
			Type   string `json:"type"`
			Coupon struct {
				ID     string `json:"id"`
				Object string `json:"object"`
			} `json:"coupon"`
		} `json:"source"`
		PromotionCode *string `json:"promotion_code"`
	}

	couponDiscount := getJSON[discountShape](t, handler, "/v1/subscriptions/"+subscription.ID+"/discount")
	if couponDiscount.PromotionCode != nil {
		t.Fatalf("coupon discount promotion_code = %v, want null", *couponDiscount.PromotionCode)
	}
	if couponDiscount.Source.Type != "coupon" || couponDiscount.Source.Coupon.ID != coupon.ID || couponDiscount.Source.Coupon.Object != "coupon" {
		t.Fatalf("coupon source = %#v, want type=coupon with %s", couponDiscount.Source, coupon.ID)
	}

	// A promotion-code discount keeps the coupon as its source and names the
	// promotion code beside it.
	promo := postForm[struct {
		ID string `json:"id"`
	}](t, handler, "/v1/promotion_codes", url.Values{
		"coupon": {coupon.ID},
		"code":   {"SDK31"},
	})
	promoSub := postForm[prorationSubResponse](t, handler, "/v1/subscriptions", url.Values{
		"customer":                     {customer.ID},
		"items[0][price]":              {price.ID},
		"items[0][quantity]":           {"1"},
		"discounts[0][promotion_code]": {promo.ID},
	})
	promoDiscount := getJSON[discountShape](t, handler, "/v1/subscriptions/"+promoSub.ID+"/discount")
	if promoDiscount.Source.Type != "coupon" || promoDiscount.Source.Coupon.ID != coupon.ID || promoDiscount.PromotionCode == nil || *promoDiscount.PromotionCode != promo.ID {
		t.Fatalf("promotion code discount = %#v, want coupon %s source with promotion_code %s", promoDiscount, coupon.ID, promo.ID)
	}
}

// transfer_lookup_key moves an in-use lookup key onto the updated price,
// clearing it from the price that held it.
func TestPriceTransferLookupKey(t *testing.T) {
	handler := newTestHandler(t)
	product := postForm[billing.Product](t, handler, "/v1/products", url.Values{"name": {"Lookup Plan"}})
	holder := postForm[billing.Price](t, handler, "/v1/prices", url.Values{
		"product":             {product.ID},
		"currency":            {"usd"},
		"unit_amount":         {"1000"},
		"lookup_key":          {"plan-standard"},
		"recurring[interval]": {"month"},
	})
	other := postForm[billing.Price](t, handler, "/v1/prices", url.Values{
		"product":             {product.ID},
		"currency":            {"usd"},
		"unit_amount":         {"2000"},
		"recurring[interval]": {"month"},
	})

	transferred := postForm[billing.Price](t, handler, "/v1/prices/"+other.ID, url.Values{
		"lookup_key":          {"plan-standard"},
		"transfer_lookup_key": {"true"},
	})
	if transferred.LookupKey != "plan-standard" {
		t.Fatalf("transferred lookup_key = %q, want plan-standard", transferred.LookupKey)
	}
	afterHolder := getJSON[billing.Price](t, handler, "/v1/prices/"+holder.ID)
	if afterHolder.LookupKey != "" {
		t.Fatalf("previous holder lookup_key = %q, want cleared", afterHolder.LookupKey)
	}

	// Without the transfer flag the update still accepts the key but does not
	// clear other prices (bounded local semantics, documented in COMPATIBILITY.md).
	plain := postForm[billing.Price](t, handler, "/v1/prices/"+holder.ID, url.Values{
		"lookup_key": {"plan-standard"},
	})
	if plain.LookupKey != "plan-standard" {
		t.Fatalf("plain update lookup_key = %q, want plan-standard", plain.LookupKey)
	}
}
