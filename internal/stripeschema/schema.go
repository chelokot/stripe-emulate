// Package stripeschema checks emulator payloads against Stripe's published
// object schemas for API version 2025-12-15.clover (github.com/stripe/openapi
// at ed93b94, trimmed by ./extract).
package stripeschema

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ExtensionPrefix marks emulator-only fields that Stripe never returns.
const ExtensionPrefix = "emulator_"

//go:embed schemas.json.gz
var compressedSchemas []byte

// liveOnlyFields are returned by live Stripe test mode for 2025-12-15.clover
// but missing from its public spec; internal/parity compares them value for
// value. Keys name a component schema, or schema.property for inline ones.
var liveOnlyFields = map[string][]string{
	"billing_bill_resource_invoicing_lines_parents_invoice_line_item_parent": {"license_fee_subscription_details"},
	"checkout.session": {"allowed_payment_method_types", "integration_identifier", "managed_payments", "marketing_attributions"},
	"invoice.lines":    {"total_count"},
	"invoiceitem":      {"frozen_fields", "invoicing_rules", "quantity_decimal"},
	"item":             {"adjustable_quantity"},
	"line_item":        {"quantity_decimal", "subtotal"},
	"payment_method":   {"shared_payment_granted_token"},
	"product":          {"attributes", "tax_details", "type"},
	"recurring":        {"trial_period_days"},
}

type schema struct {
	Ref                  string             `json:"$ref"`
	Type                 string             `json:"type"`
	Nullable             bool               `json:"nullable"`
	Properties           map[string]*schema `json:"properties"`
	Required             []string           `json:"required"`
	Items                *schema            `json:"items"`
	AnyOf                []*schema          `json:"anyOf"`
	OneOf                []*schema          `json:"oneOf"`
	AllOf                []*schema          `json:"allOf"`
	Enum                 []any              `json:"enum"`
	AdditionalProperties json.RawMessage    `json:"additionalProperties"`
}

type Validator struct {
	schemas map[string]*schema
}

func New() (Validator, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressedSchemas))
	if err != nil {
		return Validator{}, err
	}
	var schemas map[string]*schema
	if err := json.NewDecoder(reader).Decode(&schemas); err != nil {
		return Validator{}, err
	}
	return Validator{schemas: schemas}, nil
}

// Response reports schema violations in a Stripe API response body: error
// envelopes against the error schema, and every Stripe object found in a
// success body against the schema its "object" names.
func (v Validator) Response(status int, body any) []string {
	if status >= 400 {
		return v.check("$", body, v.schemas["error"], "error")
	}
	return v.objects("$", body)
}

// objects validates every Stripe object reachable from value, descending
// through lists and free-form containers such as event.data.object.
func (v Validator) objects(path string, value any) []string {
	switch typed := value.(type) {
	case []any:
		var violations []string
		for index, item := range typed {
			violations = append(violations, v.objects(fmt.Sprintf("%s[%d]", path, index), item)...)
		}
		return violations
	case map[string]any:
		if name := stripeObjectName(typed); name != "" {
			resolved, ok := v.schemas[name]
			if !ok {
				return []string{fmt.Sprintf("%s: unknown object %q", path, name)}
			}
			return v.check(path, typed, resolved, name)
		}
		var violations []string
		for _, key := range sortedKeys(typed) {
			if !strings.HasPrefix(key, ExtensionPrefix) {
				violations = append(violations, v.objects(path+"."+key, typed[key])...)
			}
		}
		return violations
	default:
		return nil
	}
}

// check validates value against s; name identifies s for liveOnlyFields.
func (v Validator) check(path string, value any, s *schema, name string) []string {
	if s.Ref != "" {
		target := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		return v.check(path, value, v.schemas[target], target)
	}
	if value == nil {
		if s.Nullable {
			return nil
		}
		return []string{path + ": null is not allowed"}
	}
	if len(s.AnyOf)+len(s.OneOf) > 0 {
		return v.alternatives(path, value, append(append([]*schema{}, s.AnyOf...), s.OneOf...), name)
	}
	var violations []string
	for _, part := range s.AllOf {
		violations = append(violations, v.check(path, value, part, name)...)
	}
	if len(s.Enum) > 0 && !slices.Contains(s.Enum, value) {
		violations = append(violations, fmt.Sprintf("%s: %v is not one of %v", path, value, s.Enum))
	}
	switch s.Type {
	case "string":
		if _, ok := value.(string); !ok {
			violations = append(violations, fmt.Sprintf("%s: %T is not a string", path, value))
		}
	case "integer":
		if number, ok := value.(float64); !ok || number != float64(int64(number)) {
			violations = append(violations, fmt.Sprintf("%s: %v is not an integer", path, value))
		}
	case "number":
		if _, ok := value.(float64); !ok {
			violations = append(violations, fmt.Sprintf("%s: %T is not a number", path, value))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			violations = append(violations, fmt.Sprintf("%s: %T is not a boolean", path, value))
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return append(violations, fmt.Sprintf("%s: %T is not an array", path, value))
		}
		for index, item := range items {
			violations = append(violations, v.check(fmt.Sprintf("%s[%d]", path, index), item, s.Items, name+"[]")...)
		}
	case "object":
		violations = append(violations, v.checkObject(path, value, s, name)...)
	}
	return violations
}

func (v Validator) checkObject(path string, value any, s *schema, name string) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s: %T is not an object", path, value)}
	}
	var additional *schema
	if len(s.AdditionalProperties) > 0 && string(s.AdditionalProperties) != "false" {
		additional = &schema{}
		if err := json.Unmarshal(s.AdditionalProperties, additional); err != nil {
			return []string{fmt.Sprintf("%s: invalid additionalProperties schema: %v", path, err)}
		}
	}
	if s.Properties == nil && additional == nil {
		return v.objects(path, object)
	}
	var violations []string
	for _, key := range s.Required {
		if _, ok := object[key]; !ok {
			violations = append(violations, fmt.Sprintf("%s.%s: required %s field is missing", path, key, name))
		}
	}
	for _, key := range sortedKeys(object) {
		property, known := s.Properties[key]
		switch {
		case known:
			violations = append(violations, v.check(path+"."+key, object[key], property, name+"."+key)...)
		case additional != nil:
			violations = append(violations, v.check(path+"."+key, object[key], additional, name+"."+key)...)
		case !strings.HasPrefix(key, ExtensionPrefix) && !slices.Contains(liveOnlyFields[name], key):
			violations = append(violations, fmt.Sprintf("%s.%s: field is not in the Stripe %s schema", path, key, name))
		}
	}
	return violations
}

// alternatives passes when any option matches; otherwise it reports the
// option whose type fits value, so an expanded object is not judged as an id.
func (v Validator) alternatives(path string, value any, options []*schema, name string) []string {
	if objectName := stripeObjectName(value); objectName != "" {
		for _, option := range options {
			if option.Ref == "#/components/schemas/"+objectName {
				return v.check(path, value, option, name)
			}
		}
	}
	var closest []string
	closestFits := false
	for index, option := range options {
		violations := v.check(path, value, option, name)
		if len(violations) == 0 {
			return nil
		}
		fits := !strings.HasPrefix(violations[0], path+": ")
		if index == 0 || (fits && !closestFits) || (fits == closestFits && len(violations) < len(closest)) {
			closest, closestFits = violations, fits
		}
	}
	return closest
}

// stripeObjectName names the component schema of a Stripe object, or returns
// "" for values that are not objects or are list envelopes.
func stripeObjectName(value any) string {
	object, _ := value.(map[string]any)
	name, _ := object["object"].(string)
	if name == "list" || name == "search_result" {
		return ""
	}
	if name != "" && object["deleted"] == true {
		return "deleted_" + name
	}
	return name
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
