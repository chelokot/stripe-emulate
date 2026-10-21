package parity

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var stripeIDPattern = regexp.MustCompile(`^((?:[a-z]+_)+)[A-Za-z0-9]{8,}$`)

const minTimestamp = 1_000_000_000

var dashboardOnlyFields = map[string]bool{"request_log_url": true}

var randomFields = map[string]bool{"invoice_prefix": true, "fingerprint": true}

var accountSettingFields = map[string]bool{"adaptive_pricing": true, "branding_settings": true, "payment_method_configuration_details": true, "payment_method_types": true}

type differ struct {
	stripeToEmulator map[string]string
	emulatorToStripe map[string]string
	diffs            []string
}

func newDiffer() *differ {
	return &differ{stripeToEmulator: map[string]string{}, emulatorToStripe: map[string]string{}}
}

func (d *differ) compare(path string, stripe any, emulator any) {
	switch stripeValue := stripe.(type) {
	case map[string]any:
		emulatorValue, ok := emulator.(map[string]any)
		if !ok {
			d.report(path, stripe, emulator)
			return
		}
		keys := map[string]bool{}
		for key := range stripeValue {
			keys[key] = true
		}
		for key := range emulatorValue {
			keys[key] = true
		}
		for _, key := range sortedKeys(keys) {
			if dashboardOnlyFields[key] {
				continue
			}
			stripeField, inStripe := stripeValue[key]
			emulatorField, inEmulator := emulatorValue[key]
			switch {
			case !inEmulator:
				d.diffs = append(d.diffs, fmt.Sprintf("%s.%s: missing in emulator (stripe: %s)", path, key, brief(stripeField)))
			case !inStripe:
				d.diffs = append(d.diffs, fmt.Sprintf("%s.%s: not in stripe (emulator: %s)", path, key, brief(emulatorField)))
			case randomFields[key] || accountSettingFields[key]:
				if fmt.Sprintf("%T", stripeField) != fmt.Sprintf("%T", emulatorField) {
					d.report(path+"."+key, stripeField, emulatorField)
				}
			default:
				d.compare(path+"."+key, stripeField, emulatorField)
			}
		}
	case []any:
		emulatorValue, ok := emulator.([]any)
		if !ok || len(stripeValue) != len(emulatorValue) {
			d.report(path, stripe, emulator)
			return
		}
		for index := range stripeValue {
			d.compare(fmt.Sprintf("%s[%d]", path, index), stripeValue[index], emulatorValue[index])
		}
	case string:
		emulatorValue, ok := emulator.(string)
		if !ok || !d.equivalentStrings(stripeValue, emulatorValue) {
			d.report(path, stripe, emulator)
		}
	case float64:
		emulatorValue, ok := emulator.(float64)
		if !ok || (stripeValue != emulatorValue && (stripeValue < minTimestamp || emulatorValue < minTimestamp)) {
			d.report(path, stripe, emulator)
		}
	default:
		if stripe != emulator {
			d.report(path, stripe, emulator)
		}
	}
}

func (d *differ) equivalentStrings(stripe string, emulator string) bool {
	if stripe == emulator {
		return true
	}
	if isURL(stripe) && isURL(emulator) {
		return true
	}
	if strings.HasPrefix(stripe, "/v1/") && strings.HasPrefix(emulator, "/v1/") {
		stripeSegments, emulatorSegments := strings.Split(stripe, "/"), strings.Split(emulator, "/")
		if len(stripeSegments) != len(emulatorSegments) {
			return false
		}
		for index := range stripeSegments {
			if !d.equivalentStrings(stripeSegments[index], emulatorSegments[index]) {
				return false
			}
		}
		return true
	}
	if stripeSecret, emulatorSecret := strings.Index(stripe, "_secret_"), strings.Index(emulator, "_secret_"); stripeSecret > 0 && emulatorSecret > 0 {
		return d.equivalentIDs(stripe[:stripeSecret], emulator[:emulatorSecret])
	}
	return d.equivalentIDs(stripe, emulator)
}

func (d *differ) equivalentIDs(stripe string, emulator string) bool {
	stripeMatch := stripeIDPattern.FindStringSubmatch(stripe)
	emulatorMatch := stripeIDPattern.FindStringSubmatch(emulator)
	if stripeMatch == nil || emulatorMatch == nil || stripeMatch[1] != emulatorMatch[1] {
		return false
	}
	mappedEmulator, stripeSeen := d.stripeToEmulator[stripe]
	mappedStripe, emulatorSeen := d.emulatorToStripe[emulator]
	if !stripeSeen && !emulatorSeen {
		d.stripeToEmulator[stripe] = emulator
		d.emulatorToStripe[emulator] = stripe
		return true
	}
	return mappedEmulator == emulator && mappedStripe == stripe
}

func (d *differ) report(path string, stripe any, emulator any) {
	d.diffs = append(d.diffs, fmt.Sprintf("%s: stripe %s, emulator %s", path, brief(stripe), brief(emulator)))
}

func isURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}

func brief(value any) string {
	text := fmt.Sprintf("%#v", value)
	if len(text) > 160 {
		return text[:160] + "…"
	}
	return text
}

func sortedKeys(keys map[string]bool) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
