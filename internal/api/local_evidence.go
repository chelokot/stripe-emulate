package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chelokot/stripe-emulate/internal/billing"
	"github.com/chelokot/stripe-emulate/internal/webhooks"
)

// Evidence kinds. These are the persistence keys, so renaming one orphans the
// rows already written under the old name.
const (
	kindCoupon              = "coupon"
	kindPromotionCode       = "promotion_code"
	kindSchedule            = "schedule"
	kindDispute             = "dispute"
	kindTaxRate             = "tax_rate"
	kindTaxID               = "tax_id"
	kindCash                = "cash"
	kindPortalConfiguration = "portal_configuration"
)

// LocalEvidenceRepository persists evidence objects in the run's own store, so a
// run backed by a file keeps them across restarts and an in-memory run does not.
type LocalEvidenceRepository interface {
	SaveLocalEvidence(ctx context.Context, kind, id, data string) error
	DeleteLocalEvidence(ctx context.Context, kind, id string) error
	LoadLocalEvidence(ctx context.Context) (map[string]map[string]string, error)
}

type localEvidenceStore struct {
	mu                   sync.Mutex
	repo                 LocalEvidenceRepository
	coupons              map[string]map[string]any
	promotionCodes       map[string]map[string]any
	schedules            map[string]map[string]any
	cashBalances         map[string]int64
	cashTxs              map[string][]map[string]any
	disputes             map[string]map[string]any
	taxRates             map[string]map[string]any
	taxIDs               map[string]map[string]any
	portalConfigurations map[string]map[string]any
}

// newLocalEvidenceStore returns an evidence store. A nil repo keeps everything in
// memory, which is what callers without a store (scorecard runs, unit tests) want.
func newLocalEvidenceStore(repo LocalEvidenceRepository) *localEvidenceStore {
	s := &localEvidenceStore{
		repo:                 repo,
		coupons:              map[string]map[string]any{},
		promotionCodes:       map[string]map[string]any{},
		schedules:            map[string]map[string]any{},
		cashBalances:         map[string]int64{},
		cashTxs:              map[string][]map[string]any{},
		disputes:             map[string]map[string]any{},
		taxRates:             map[string]map[string]any{},
		taxIDs:               map[string]map[string]any{},
		portalConfigurations: map[string]map[string]any{},
	}
	s.restore()
	return s
}

func (s *localEvidenceStore) mapFor(kind string) map[string]map[string]any {
	switch kind {
	case kindCoupon:
		return s.coupons
	case kindPromotionCode:
		return s.promotionCodes
	case kindSchedule:
		return s.schedules
	case kindDispute:
		return s.disputes
	case kindTaxRate:
		return s.taxRates
	case kindTaxID:
		return s.taxIDs
	case kindPortalConfiguration:
		return s.portalConfigurations
	}
	return nil
}

// saveLocked records obj and mirrors it to the repo. The caller holds mu.
func (s *localEvidenceStore) saveLocked(kind, id string, obj map[string]any) error {
	if m := s.mapFor(kind); m != nil {
		m[id] = obj
	}
	if s.repo == nil {
		return nil
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return s.repo.SaveLocalEvidence(context.Background(), kind, id, string(data))
}

func (s *localEvidenceStore) save(kind, id string, obj map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(kind, id, obj)
}

func (s *localEvidenceStore) remove(kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.mapFor(kind); m != nil {
		delete(m, id)
	}
	if s.repo == nil {
		return nil
	}
	return s.repo.DeleteLocalEvidence(context.Background(), kind, id)
}

// cashRecord is the persisted shape of one customer's cash balance and its ledger.
type cashRecord struct {
	Balance      int64            `json:"balance"`
	Transactions []map[string]any `json:"transactions"`
}

// addCash moves the balance and appends the transaction as one unit — the two are
// read together by GET /v1/customers/<id>/cash_balance, so a partial write would
// show a balance no ledger explains.
func (s *localEvidenceStore) addCash(customerID string, amount int64, tx map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cashBalances[customerID] += amount
	tx["ending_balance"] = s.cashBalances[customerID]
	s.cashTxs[customerID] = append(s.cashTxs[customerID], tx)
	if s.repo == nil {
		return nil
	}
	data, err := json.Marshal(cashRecord{Balance: s.cashBalances[customerID], Transactions: s.cashTxs[customerID]})
	if err != nil {
		return err
	}
	return s.repo.SaveLocalEvidence(context.Background(), kindCash, customerID, string(data))
}

// restore reloads persisted evidence. A store that cannot be read is left empty
// rather than failing the process — the run still serves, it just has no history.
func (s *localEvidenceStore) restore() {
	if s.repo == nil {
		return
	}
	all, err := s.repo.LoadLocalEvidence(context.Background())
	if err != nil {
		return
	}
	for kind, rows := range all {
		for id, raw := range rows {
			if kind == kindCash {
				var rec cashRecord
				if json.Unmarshal([]byte(raw), &rec) == nil {
					s.cashBalances[id] = rec.Balance
					s.cashTxs[id] = rec.Transactions
				}
				continue
			}
			m := s.mapFor(kind)
			if m == nil {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(raw), &obj) == nil {
				m[id] = obj
			}
		}
	}
}

func (h *Handler) handleCoupons(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateCouponCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		now := time.Now().UTC()
		id := p.string("id")
		if id == "" {
			id = "coupon_" + strconv.FormatInt(now.UnixNano(), 36)
		}
		coupon := map[string]any{
			"id":                 id,
			"object":             "coupon",
			"name":               emptyToNil(p.string("name")),
			"duration":           p.stringDefault("duration", "once"),
			"percent_off":        nil,
			"amount_off":         nil,
			"currency":           emptyToNil(p.string("currency")),
			"duration_in_months": nil,
			"max_redemptions":    nil,
			"redeem_by":          nil,
			"times_redeemed":     int64(0),
			"valid":              true,
			"metadata":           nonNilMap(p.metadata()),
			"created":            now.Unix(),
			"livemode":           false,
		}
		if p.has("percent_off") {
			coupon["percent_off"] = p.float64("percent_off")
		}
		if p.has("amount_off") {
			coupon["amount_off"] = p.int64("amount_off")
		}
		if p.has("duration_in_months") {
			coupon["duration_in_months"] = p.int64("duration_in_months")
		}
		if p.has("max_redemptions") {
			coupon["max_redemptions"] = p.int64("max_redemptions")
		}
		if p.has("redeem_by") {
			coupon["redeem_by"] = p.int64("redeem_by")
		}
		if products := p.appliesToProducts(); len(products) > 0 {
			coupon["applies_to"] = map[string]any{"products": products}
		}
		if err := h.local.save(kindCoupon, id, coupon); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cloneEvidence(coupon))
	case http.MethodGet:
		h.local.mu.Lock()
		data := evidenceList(h.local.coupons)
		h.local.mu.Unlock()
		writeJSON(w, http.StatusOK, stripeList(r.URL.Path, data))
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

func (h *Handler) handleCoupon(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/coupons/"), "/")
	if id == "" || strings.Contains(id, "/") {
		h.notFound(w, r)
		return
	}
	h.local.mu.Lock()
	coupon, ok := h.local.coupons[id]
	h.local.mu.Unlock()
	if !ok {
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		writeJSON(w, http.StatusOK, cloneEvidence(coupon))
	case http.MethodDelete:
		deleted := map[string]any{"id": id, "object": "coupon", "deleted": true}
		if err := h.local.remove(kindCoupon, id); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, deleted)
	default:
		h.methodNotAllowed(w, r, "GET, POST, DELETE")
	}
}

func (h *Handler) handlePromotionCodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validatePromotionCodeCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		now := time.Now().UTC()
		id := p.string("id")
		if id == "" {
			id = "promo_" + strconv.FormatInt(now.UnixNano(), 36)
		}
		code := p.string("code")
		if code == "" {
			code = strings.ToUpper(id)
		}
		h.local.mu.Lock()
		coupon, couponOK := h.local.coupons[p.string("coupon")]
		h.local.mu.Unlock()
		if !couponOK {
			writeResult(w, nil, billing.ErrNotFound)
			return
		}
		promo := promotionCodeEvidence(id, code, coupon, p.boolDefault("active", true), p.string("customer"), p.metadata(), now)
		if err := h.local.save(kindPromotionCode, id, promo); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cloneEvidence(promo))
	case http.MethodGet:
		h.local.mu.Lock()
		data := evidenceList(h.local.promotionCodes)
		h.local.mu.Unlock()
		data = filterPromotionCodeEvidence(data, r)
		writeJSON(w, http.StatusOK, stripeList(r.URL.Path, data))
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

func (h *Handler) handlePromotionCode(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/promotion_codes/"), "/")
	if id == "" || strings.Contains(id, "/") {
		h.notFound(w, r)
		return
	}
	h.local.mu.Lock()
	promo, ok := h.local.promotionCodes[id]
	h.local.mu.Unlock()
	if !ok {
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		writeJSON(w, http.StatusOK, cloneEvidence(promo))
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

func promotionCodeEvidence(id string, code string, coupon map[string]any, active bool, customerID string, metadata map[string]string, now time.Time) map[string]any {
	return map[string]any{
		"id":               id,
		"object":           "promotion_code",
		"active":           active,
		"code":             code,
		"created":          now.Unix(),
		"customer":         emptyToNil(customerID),
		"customer_account": nil,
		"expires_at":       nil,
		"livemode":         false,
		"max_redemptions":  nil,
		"metadata":         nonNilMap(metadata),
		"promotion":        map[string]any{"type": "coupon", "coupon": coupon["id"]},
		"restrictions":     map[string]any{"first_time_transaction": false, "minimum_amount": nil, "minimum_amount_currency": nil},
		"times_redeemed":   int64(0),
	}
}

func promotionCouponID(promo map[string]any) string {
	promotion, _ := promo["promotion"].(map[string]any)
	couponID, _ := promotion["coupon"].(string)
	return couponID
}

func (h *Handler) handleTaxRates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateTaxRateCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		now := time.Now().UTC()
		id := "txr_" + strconv.FormatInt(now.UnixNano(), 36)
		percentage, _ := strconv.ParseFloat(p.string("percentage"), 64)
		taxRate := map[string]any{
			"id":                   id,
			"object":               "tax_rate",
			"display_name":         p.string("display_name"),
			"percentage":           percentage,
			"inclusive":            p.boolDefault("inclusive", false),
			"active":               p.boolDefault("active", true),
			"country":              emptyToNil(p.string("country")),
			"state":                emptyToNil(p.string("state")),
			"jurisdiction":         emptyToNil(p.string("jurisdiction")),
			"description":          emptyToNil(p.string("description")),
			"effective_percentage": nil,
			"flat_amount":          nil,
			"jurisdiction_level":   nil,
			"rate_type":            "percentage",
			"tax_type":             nil,
			"metadata":             nonNilMap(p.metadata()),
			"created":              now.Unix(),
			"livemode":             false,
		}
		if err := h.local.save(kindTaxRate, id, taxRate); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cloneEvidence(taxRate))
	case http.MethodGet:
		h.local.mu.Lock()
		data := evidenceList(h.local.taxRates)
		h.local.mu.Unlock()
		writeJSON(w, http.StatusOK, stripeList(r.URL.Path, data))
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

func (h *Handler) handleTaxRate(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/tax_rates/"), "/")
	if id == "" || strings.Contains(id, "/") {
		h.notFound(w, r)
		return
	}
	h.local.mu.Lock()
	taxRate, ok := h.local.taxRates[id]
	h.local.mu.Unlock()
	if !ok {
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, cloneEvidence(taxRate))
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateTaxRateUpdate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		h.local.mu.Lock()
		current := h.local.taxRates[id]
		if p.has("active") {
			current["active"] = p.boolDefault("active", true)
		}
		if p.has("display_name") {
			current["display_name"] = p.string("display_name")
		}
		if p.has("description") {
			current["description"] = emptyToNil(p.string("description"))
		}
		if metadata := p.metadata(); metadata != nil {
			merged := map[string]string{}
			if existing, ok := current["metadata"].(map[string]string); ok {
				for key, value := range existing {
					merged[key] = value
				}
			} else if existing, ok := current["metadata"].(map[string]any); ok {
				for key, value := range existing {
					merged[key] = fmt.Sprint(value)
				}
			}
			for key, value := range metadata {
				merged[key] = value
			}
			current["metadata"] = nonNilMap(merged)
		}
		err = h.local.saveLocked(kindTaxRate, id, current)
		h.local.mu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cloneEvidence(current))
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

func (h *Handler) handleCustomerTaxIDs(w http.ResponseWriter, r *http.Request, customerID string, taxID string) {
	customer, err := h.billing.GetCustomer(r.Context(), customerID)
	if err := validateCustomerExists(customer, err); err != nil {
		writeResult(w, nil, err)
		return
	}
	if taxID == "" {
		switch r.Method {
		case http.MethodPost:
			p, err := parseParams(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := validateCustomerTaxIDCreate(p); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			now := time.Now().UTC()
			id := "txi_" + strconv.FormatInt(now.UnixNano(), 36)
			taxIDObj := map[string]any{
				"id":               id,
				"object":           "tax_id",
				"country":          nil,
				"created":          now.Unix(),
				"customer":         customer.ID,
				"customer_account": nil,
				"livemode":         false,
				"type":             p.string("type"),
				"value":            p.string("value"),
				"owner": map[string]any{
					"type":             "customer",
					"customer":         customer.ID,
					"customer_account": nil,
				},
				"verification": map[string]any{
					"status":           "verified",
					"verified_address": nil,
					"verified_name":    nil,
				},
			}
			if err := h.local.save(kindTaxID, id, taxIDObj); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, cloneEvidence(taxIDObj))
		case http.MethodGet:
			h.local.mu.Lock()
			data := make([]map[string]any, 0)
			for _, item := range h.local.taxIDs {
				if fmt.Sprint(item["customer"]) == customer.ID {
					data = append(data, cloneEvidence(item))
				}
			}
			h.local.mu.Unlock()
			writeJSON(w, http.StatusOK, stripeList(r.URL.Path, data))
		default:
			h.methodNotAllowed(w, r, "GET, POST")
		}
		return
	}
	if strings.Contains(taxID, "/") {
		h.notFound(w, r)
		return
	}
	h.local.mu.Lock()
	item, ok := h.local.taxIDs[taxID]
	h.local.mu.Unlock()
	if !ok || fmt.Sprint(item["customer"]) != customer.ID {
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, cloneEvidence(item))
	case http.MethodDelete:
		if err := h.local.remove(kindTaxID, taxID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, stripeDeleted(taxID, "tax_id"))
	default:
		h.methodNotAllowed(w, r, "GET, DELETE")
	}
}

func filterPromotionCodeEvidence(data []map[string]any, r *http.Request) []map[string]any {
	query := r.URL.Query()
	code := strings.TrimSpace(query.Get("code"))
	couponID := strings.TrimSpace(query.Get("coupon"))
	customerID := strings.TrimSpace(query.Get("customer"))
	activeFilter := strings.TrimSpace(query.Get("active"))
	if code == "" && couponID == "" && customerID == "" && activeFilter == "" {
		return data
	}
	out := make([]map[string]any, 0, len(data))
	for _, promo := range data {
		if code != "" && !strings.EqualFold(fmt.Sprint(promo["code"]), code) {
			continue
		}
		if couponID != "" && promotionCouponID(promo) != couponID {
			continue
		}
		if customerID != "" && fmt.Sprint(promo["customer"]) != customerID {
			continue
		}
		if activeFilter != "" {
			wantActive := activeFilter == "true" || activeFilter == "1"
			active, _ := promo["active"].(bool)
			if active != wantActive {
				continue
			}
		}
		out = append(out, promo)
	}
	return out
}

func (h *Handler) handleSubscriptionSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateSubscriptionScheduleCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		subscriptionID := p.first("from_subscription", "subscription")
		subscription, err := h.billing.GetSubscription(r.Context(), subscriptionID)
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		now := time.Now().UTC()
		id := p.string("id")
		if id == "" {
			id = "sub_sched_" + strconv.FormatInt(now.UnixNano(), 36)
		}
		startDate := p.int64Default("phases[0][start_date]", subscription.CurrentPeriodEnd.Unix())
		priceID := p.first("phases[0][items][0][price]", "phases[0][plans][0][price]")
		if priceID == "" {
			writeError(w, http.StatusBadRequest, missingParam("phases[0][items][0][price]"))
			return
		}
		price, err := h.billing.GetPrice(r.Context(), priceID)
		if err := validatePriceExists(price, err); err != nil {
			writeResult(w, nil, err)
			return
		}
		quantity := p.int64Default("phases[0][items][0][quantity]", 1)
		if quantity <= 0 {
			writeError(w, http.StatusBadRequest, invalidParam("phases[0][items][0][quantity]", "Must be at least 1."))
			return
		}
		start := time.Unix(startDate, 0).UTC()
		endDate := start.AddDate(0, 1, 0).Unix()
		if price.RecurringInterval == "year" {
			endDate = start.AddDate(1, 0, 0).Unix()
		}
		schedule := map[string]any{
			"id":            id,
			"object":        "subscription_schedule",
			"billing_mode":  map[string]any{"flexible": nil, "type": "classic"},
			"canceled_at":   nil,
			"completed_at":  nil,
			"created":       now.Unix(),
			"current_phase": nil,
			"customer":      subscription.CustomerID,
			"default_settings": map[string]any{
				"automatic_tax":        map[string]any{"disabled_reason": nil, "enabled": false, "liability": nil},
				"billing_cycle_anchor": "automatic",
				"invoice_settings":     map[string]any{"account_tax_ids": nil, "days_until_due": nil, "issuer": map[string]any{"type": "self"}},
			},
			"end_behavior": p.stringDefault("end_behavior", "release"),
			"livemode":     false,
			"metadata":     nonNilMap(p.metadata()),
			"phases": []map[string]any{{
				"add_invoice_items": []any{},
				"currency":          price.Currency,
				"discounts":         []any{},
				"end_date":          endDate,
				"items": []map[string]any{{
					"discounts": []any{},
					"metadata":  map[string]string{},
					"price":     priceID,
					"quantity":  quantity,
				}},
				"metadata":           map[string]string{},
				"proration_behavior": "create_prorations",
				"start_date":         startDate,
			}},
			"released_at":           nil,
			"released_subscription": nil,
			"status":                "active",
			"subscription":          subscription.ID,
			"test_clock":            nil,
		}
		if err := h.local.save(kindSchedule, id, schedule); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cloneEvidence(schedule))
	case http.MethodGet:
		h.local.mu.Lock()
		data := evidenceList(h.local.schedules)
		h.local.mu.Unlock()
		writeJSON(w, http.StatusOK, stripeList(r.URL.Path, data))
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

func (h *Handler) handleSubscriptionSchedule(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/subscription_schedules/"), "/")
	id, action, hasAction := strings.Cut(rest, "/")
	if id == "" {
		h.notFound(w, r)
		return
	}
	h.local.mu.Lock()
	schedule, ok := h.local.schedules[id]
	h.local.mu.Unlock()
	if !ok {
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	if !hasAction {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			h.methodNotAllowed(w, r, "GET, POST")
			return
		}
		writeJSON(w, http.StatusOK, cloneEvidence(schedule))
		return
	}
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, r, "POST")
		return
	}
	switch action {
	case "cancel":
		schedule["status"] = "canceled"
	case "release":
		schedule["status"] = "released"
	default:
		h.notFound(w, r)
		return
	}
	if err := h.local.save(kindSchedule, id, schedule); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cloneEvidence(schedule))
}

func (h *Handler) applyDueSubscriptionSchedules(r *http.Request, clockID string, at time.Time) []billing.Subscription {
	h.local.mu.Lock()
	schedules := evidenceList(h.local.schedules)
	h.local.mu.Unlock()

	var updated []billing.Subscription
	for _, schedule := range schedules {
		if schedule["status"] != "active" {
			continue
		}
		phases, _ := schedule["phases"].([]map[string]any)
		startDate := int64(0)
		if len(phases) > 0 {
			startDate, _ = asInt64Evidence(phases[0]["start_date"])
		}
		if startDate == 0 || startDate > at.Unix() {
			continue
		}
		subscriptionID, _ := schedule["subscription"].(string)
		priceID, quantity := schedulePhaseItem(schedule)
		if subscriptionID == "" || priceID == "" {
			continue
		}
		if !h.subscriptionAttachedToClock(r, subscriptionID, clockID) {
			continue
		}
		subscription, err := h.billing.PatchSubscription(r.Context(), subscriptionID, billing.SubscriptionPatch{
			Items:        []billing.LineItem{{PriceID: priceID, Quantity: quantity}},
			ReplaceItems: true,
			Metadata:     map[string]string{"emulator_subscription_schedule": fmt.Sprint(schedule["id"])},
		})
		if err != nil {
			continue
		}
		schedule["status"] = "completed"
		// Best effort: this runs inside a clock advance, which has no response to fail.
		_ = h.local.save(kindSchedule, fmt.Sprint(schedule["id"]), schedule)
		h.emitSubscriptionWebhook(r, "customer.subscription.updated", subscription, webhooks.SourceAPI)
		updated = append(updated, subscription)
	}
	return updated
}

func (h *Handler) subscriptionAttachedToClock(r *http.Request, subscriptionID string, clockID string) bool {
	clockID = strings.TrimSpace(clockID)
	if clockID == "" {
		return true
	}
	subscription, err := h.billing.GetSubscription(r.Context(), subscriptionID)
	if err != nil {
		return false
	}
	if strings.TrimSpace(subscription.Metadata[billing.MetadataTestClock]) == clockID {
		return true
	}
	customer, err := h.billing.GetCustomer(r.Context(), subscription.CustomerID)
	if err != nil {
		return false
	}
	return strings.TrimSpace(customer.Metadata[billing.MetadataTestClock]) == clockID
}

func schedulePhaseItem(schedule map[string]any) (string, int64) {
	phases, _ := schedule["phases"].([]map[string]any)
	if len(phases) == 0 {
		return "", 1
	}
	items, _ := phases[0]["items"].([]map[string]any)
	if len(items) == 0 {
		return "", 1
	}
	priceID, _ := items[0]["price"].(string)
	quantity, _ := asInt64Evidence(items[0]["quantity"])
	if quantity <= 0 {
		quantity = 1
	}
	return priceID, quantity
}

func (h *Handler) handleCustomerCashBalance(w http.ResponseWriter, r *http.Request, customerID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.methodNotAllowed(w, r, "GET, POST")
		return
	}
	if _, err := h.billing.GetCustomer(r.Context(), customerID); err != nil {
		writeResult(w, nil, err)
		return
	}
	h.local.mu.Lock()
	available := h.local.cashBalances[customerID]
	h.local.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"object":           "cash_balance",
		"available":        map[string]int64{"usd": available},
		"customer":         customerID,
		"customer_account": nil,
		"livemode":         false,
		"settings":         map[string]any{"reconciliation_mode": "automatic", "using_merchant_default": true},
	})
}

func (h *Handler) handleCustomerCashBalanceTransactions(w http.ResponseWriter, r *http.Request, customerID string, transactionID string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r, "GET")
		return
	}
	h.local.mu.Lock()
	transactions := append([]map[string]any(nil), h.local.cashTxs[customerID]...)
	h.local.mu.Unlock()
	if transactionID != "" {
		for _, tx := range transactions {
			if tx["id"] == transactionID {
				writeJSON(w, http.StatusOK, cloneEvidence(tx))
				return
			}
		}
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, stripeList(r.URL.Path, transactions))
}

func (h *Handler) handleTestHelperCustomer(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/test_helpers/customers/"), "/")
	customerID, action, found := strings.Cut(rest, "/")
	if customerID == "" || !found || action != "fund_cash_balance" {
		h.notFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, r, "POST")
		return
	}
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateFundCashBalance(p); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := h.billing.GetCustomer(r.Context(), customerID); err != nil {
		writeResult(w, nil, err)
		return
	}
	amount := p.int64("amount")
	now := time.Now().UTC()
	tx := map[string]any{
		"id":               "ccsbtxn_" + strconv.FormatInt(now.UnixNano(), 36),
		"object":           "customer_cash_balance_transaction",
		"customer":         customerID,
		"customer_account": nil,
		"type":             "funded",
		"net_amount":       amount,
		"currency":         strings.ToLower(p.stringDefault("currency", "usd")),
		"created":          now.Unix(),
		"livemode":         false,
	}
	if err := h.local.addCash(customerID, amount, tx); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	settled, _ := h.billing.SettleBankTransferPaymentIntents(r.Context(), customerID)
	for _, intent := range settled {
		h.emitPaymentIntentWebhook(r, "payment_intent.succeeded", intent)
	}
	writeJSON(w, http.StatusOK, tx)
}

func (h *Handler) handleDisputeSimulation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, r, "POST")
		return
	}
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateDisputeCreate(p); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dispute := h.createDispute(r, p.first("charge", "charge_id"), p.int64("amount"), p.stringDefault("reason", "general"))
	writeJSON(w, http.StatusOK, dispute)
}

func (h *Handler) handleChargeSubresource(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/charges/"), "/")
	chargeID, action, found := strings.Cut(rest, "/")
	if chargeID != "" && !found {
		h.handleCharge(w, r, chargeID)
		return
	}
	if chargeID == "" || !found || action != "dispute" {
		h.notFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		h.local.mu.Lock()
		defer h.local.mu.Unlock()
		for _, dispute := range h.local.disputes {
			if dispute["charge"] == chargeID {
				writeJSON(w, http.StatusOK, cloneEvidence(dispute))
				return
			}
		}
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, r, "GET, POST")
		return
	}
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateDisputeCreate(p); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dispute := h.createDispute(r, chargeID, p.int64("amount"), p.stringDefault("reason", "general"))
	writeJSON(w, http.StatusOK, dispute)
}

func (h *Handler) handleDisputes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r, "GET")
		return
	}
	h.local.mu.Lock()
	data := evidenceList(h.local.disputes)
	h.local.mu.Unlock()
	writeJSON(w, http.StatusOK, stripeList(r.URL.Path, data))
}

func (h *Handler) handleDispute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/disputes/"), "/")
	id, action, found := strings.Cut(rest, "/")
	if id == "" {
		h.notFound(w, r)
		return
	}
	h.local.mu.Lock()
	dispute, ok := h.local.disputes[id]
	h.local.mu.Unlock()
	if !ok {
		writeResult(w, nil, billing.ErrNotFound)
		return
	}
	if !found {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			h.methodNotAllowed(w, r, "GET, POST")
			return
		}
		if r.Method == http.MethodPost {
			p, err := parseParams(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			evidence := map[string]any{}
			if current, ok := dispute["evidence"].(map[string]any); ok {
				for key, value := range current {
					evidence[key] = value
				}
			}
			for key, value := range p.values {
				if strings.HasPrefix(key, "evidence[") && strings.HasSuffix(key, "]") {
					evidence[strings.TrimSuffix(strings.TrimPrefix(key, "evidence["), "]")] = value
				}
			}
			if status := p.string("status"); status != "" {
				dispute["status"] = status
			}
			if metadata := p.metadata(); metadata != nil {
				dispute["metadata"] = nonNilMap(metadata)
			}
			dispute["evidence"] = evidence
			details, _ := dispute["evidence_details"].(map[string]any)
			details["has_evidence"] = len(evidence) > 1
			details["submission_count"] = 1
			if err := h.local.save(kindDispute, id, dispute); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			h.emitGenericWebhook(r, "charge.dispute.updated", id, dispute, webhooks.SourceAPI)
		}
		writeJSON(w, http.StatusOK, cloneEvidence(dispute))
		return
	}
	if action != "close" || r.Method != http.MethodPost {
		h.notFound(w, r)
		return
	}
	dispute["status"] = "lost"
	if err := h.local.save(kindDispute, id, dispute); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.emitGenericWebhook(r, "charge.dispute.closed", id, dispute, webhooks.SourceAPI)
	writeJSON(w, http.StatusOK, cloneEvidence(dispute))
}

func (h *Handler) createDispute(r *http.Request, chargeID string, amount int64, reason string) map[string]any {
	now := time.Now().UTC()
	if chargeID == "" {
		chargeID = "ch_" + strconv.FormatInt(now.UnixNano(), 36)
	}
	if amount <= 0 {
		amount = 1000
	}
	dispute := disputeEvidence("dp_"+strconv.FormatInt(now.UnixNano(), 36), chargeID, amount, "usd", reason, "needs_response", nil, now)
	// Best effort: the caller returns the dispute, not an error.
	_ = h.local.save(kindDispute, fmt.Sprint(dispute["id"]), dispute)
	h.emitGenericWebhook(r, "charge.dispute.created", chargeID, dispute, webhooks.SourceAPI)
	return cloneEvidence(dispute)
}

func disputeEvidence(id string, chargeID string, amount int64, currency string, reason string, status string, metadata map[string]string, now time.Time) map[string]any {
	return map[string]any{
		"id":                         id,
		"object":                     "dispute",
		"amount":                     amount,
		"balance_transactions":       []map[string]any{},
		"charge":                     chargeID,
		"created":                    now.Unix(),
		"currency":                   currency,
		"enhanced_eligibility_types": []string{},
		"evidence":                   map[string]any{"enhanced_evidence": map[string]any{}},
		"evidence_details":           map[string]any{"due_by": nil, "enhanced_eligibility": map[string]any{}, "has_evidence": false, "past_due": false, "submission_count": 0},
		"is_charge_refundable":       true,
		"livemode":                   false,
		"metadata":                   nonNilMap(metadata),
		"payment_intent":             nil,
		"reason":                     reason,
		"status":                     status,
	}
}

func evidenceList(items map[string]map[string]any) []map[string]any {
	data := make([]map[string]any, 0, len(items))
	for _, item := range items {
		data = append(data, cloneEvidence(item))
	}
	return data
}

func cloneEvidence(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func asInt64Evidence(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func asFloat64Evidence(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
