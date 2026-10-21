package parity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDifferNormalizesIDsTimestampsAndURLs(t *testing.T) {
	d := newDiffer()
	d.compare("$", decode(t, `{"id":"cus_StripeAAAA1111","created":1790000000,"url":"https://checkout.stripe.com/c/pay/x","client_secret":"seti_StripeBBBB2222_secret_abcdefgh","customer":"cus_StripeAAAA1111"}`),
		decode(t, `{"id":"cus_emulatorAAAA1111","created":1790000500,"url":"http://127.0.0.1/checkout/x","client_secret":"seti_emulatorBBBB2222_secret_zzzzzzzz","customer":"cus_emulatorAAAA1111"}`))
	if len(d.diffs) != 0 {
		t.Fatalf("diffs = %v, want none", d.diffs)
	}
}

func TestDifferReportsShapeValueAndReferenceMismatches(t *testing.T) {
	d := newDiffer()
	d.compare("$", decode(t, `{"id":"cus_StripeAAAA1111","amount":1000,"tax":null,"customer":"cus_StripeAAAA1111","price":"price_StripeCCCC3333"}`),
		decode(t, `{"id":"cus_emulatorAAAA1111","amount":999,"extra":true,"customer":"cus_emulatorOTHER999","price":"prod_emulatorCCCC3333"}`))
	want := []string{"$.amount:", "$.extra: not in stripe", "$.id:", "$.price:", "$.tax: missing in emulator"}
	if len(d.diffs) != len(want) {
		t.Fatalf("diffs = %v, want %d entries", d.diffs, len(want))
	}
	for index, prefix := range want {
		if !strings.HasPrefix(d.diffs[index], prefix) {
			t.Fatalf("diff[%d] = %q, want prefix %q", index, d.diffs[index], prefix)
		}
	}
}

func decode(t *testing.T, body string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return value
}
