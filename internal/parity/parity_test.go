package parity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chelokot/stripe-emulate/internal/config"
	"github.com/chelokot/stripe-emulate/internal/server"
	"github.com/chelokot/stripe-emulate/internal/storage"
	"github.com/chelokot/stripe-emulate/internal/webhooks"
)

const stripeBaseURL = "https://api.stripe.com"

var placeholderPattern = regexp.MustCompile(`\{([a-z0-9_]+)((?:\.[a-z0-9_]+)*)\}`)

type side struct {
	name      string
	baseURL   string
	key       string
	responses map[string]any
	created   map[string]string
}

type response struct {
	status int
	body   any
}

func TestParity(t *testing.T) {
	key := os.Getenv("STRIPE_PARITY_KEY")
	if key == "" {
		t.Skip("STRIPE_PARITY_KEY is not set")
	}
	emulatorURL := startEmulator(t)
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	matched, total, fields := 0, 0, 0
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			stripe := &side{name: "stripe", baseURL: stripeBaseURL, key: key, responses: map[string]any{}, created: map[string]string{}}
			emulator := &side{name: "emulator", baseURL: emulatorURL, key: "sk_test_emulator", responses: map[string]any{}, created: map[string]string{}}
			t.Cleanup(func() { stripe.cleanup(t) })
			d := newDiffer()
			for _, step := range scenario.steps {
				stripeResponse := stripe.send(t, step, run)
				emulatorResponse := emulator.send(t, step, run)
				dump(t, scenario.name, step.name, stripeResponse, emulatorResponse)
				before := len(d.diffs)
				if stripeResponse.status != emulatorResponse.status {
					d.diffs = append(d.diffs, fmt.Sprintf("%s: status stripe %d, emulator %d", step.name, stripeResponse.status, emulatorResponse.status))
				}
				d.compare(step.name, stripeResponse.body, emulatorResponse.body)
				total++
				if len(d.diffs) == before {
					matched++
				}
			}
			fields += d.fields
			if len(d.diffs) > 0 {
				t.Errorf("%d differences:\n%s", len(d.diffs), strings.Join(d.diffs, "\n"))
			}
		})
	}
	t.Logf("parity: %d/%d steps match, %d response fields compared with Stripe across %d scenarios", matched, total, fields, len(scenarios))
}

func startEmulator(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "emulator.db")
	store, err := storage.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	srv := server.New(server.Options{Config: config.Config{Addr: ":0", DatabaseURL: dbPath, Environment: "test", WebhookAPIVersion: webhooks.DefaultAPIVersion}, Store: store})
	httpServer := httptest.NewServer(srv)
	t.Cleanup(func() {
		httpServer.Close()
		_ = srv.Close()
		_ = store.Close()
	})
	return httpServer.URL
}

func (s *side) send(t *testing.T, step step, run string) response {
	t.Helper()
	form := url.Values{}
	for _, pair := range step.form {
		value, err := s.resolve(pair[1], run)
		if err != nil {
			return response{body: err.Error()}
		}
		form.Add(pair[0], value)
	}
	path, err := s.resolve(step.path, run)
	if err != nil {
		return response{body: err.Error()}
	}
	target := s.baseURL + path
	var body io.Reader
	if step.method == http.MethodGet || step.method == http.MethodDelete {
		if len(form) > 0 {
			target += "?" + form.Encode()
		}
	} else {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(step.method, target, body)
	if err != nil {
		t.Fatalf("%s %s: %v", s.name, step.name, err)
	}
	request.Header.Set("Authorization", "Bearer "+s.key)
	request.Header.Set("Stripe-Version", webhooks.DefaultAPIVersion)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", s.name, step.name, err)
	}
	defer httpResponse.Body.Close()
	raw, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", s.name, step.name, err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s %s: decode %q: %v", s.name, step.name, raw, err)
	}
	s.responses[step.name] = decoded
	if object, ok := decoded.(map[string]any); ok && httpResponse.StatusCode == http.StatusOK && step.method == http.MethodPost {
		if id, ok := object["id"].(string); ok {
			s.created[id], _ = object["object"].(string)
		}
	}
	return response{status: httpResponse.StatusCode, body: decoded}
}

func (s *side) resolve(template string, run string) (string, error) {
	var unresolved error
	resolved := placeholderPattern.ReplaceAllStringFunc(template, func(placeholder string) string {
		match := placeholderPattern.FindStringSubmatch(placeholder)
		if match[1] == "run" {
			return run
		}
		value := s.responses[match[1]]
		for _, field := range strings.Split(strings.TrimPrefix(match[2], "."), ".") {
			object, ok := value.(map[string]any)
			if !ok {
				value = nil
				break
			}
			value = object[field]
		}
		text, ok := value.(string)
		if !ok {
			unresolved = fmt.Errorf("unresolved placeholder %s", placeholder)
		}
		return text
	})
	return resolved, unresolved
}

func (s *side) cleanup(t *testing.T) {
	for id, object := range s.created {
		if object != "customer" {
			continue
		}
		if result := s.send(t, request("cleanup", http.MethodDelete, "/v1/customers/"+id), ""); result.status != http.StatusOK && result.status != http.StatusNotFound {
			t.Logf("cleanup customer %s: status %d", id, result.status)
		}
	}
}

func dump(t *testing.T, scenario string, step string, stripe response, emulator response) {
	dir := os.Getenv("STRIPE_PARITY_DUMP")
	if dir == "" {
		return
	}
	for name, result := range map[string]response{"stripe": stripe, "emulator": emulator} {
		encoded, err := json.MarshalIndent(map[string]any{"status": result.status, "body": result.body}, "", "  ")
		if err != nil {
			t.Fatalf("encode dump: %v", err)
		}
		path := filepath.Join(dir, scenario, step+"."+name+".json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create dump dir: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatalf("write dump: %v", err)
		}
	}
}
