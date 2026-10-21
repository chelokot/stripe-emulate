# stripe-emulate

[![CI](https://github.com/chelokot/stripe-emulate/actions/workflows/ci.yml/badge.svg)](https://github.com/chelokot/stripe-emulate/actions/workflows/ci.yml)
[![Image](https://img.shields.io/badge/ghcr.io-chelokot%2Fstripe--emulate-2496ED?logo=docker&logoColor=white)](https://github.com/chelokot/stripe-emulate/pkgs/container/stripe-emulate)
[![Stripe API](https://img.shields.io/badge/Stripe%20API-2025--12--15.clover-635BFF?logo=stripe&logoColor=white)](https://docs.stripe.com/changelog/clover)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

A local Stripe API you can point any Stripe SDK at: subscriptions, invoices, checkout, the customer portal and signed webhooks, offline and deterministic. Its responses are diffed against live Stripe test mode and checked against Stripe's own OpenAPI schemas on every commit.

Not affiliated with Stripe. Fork of [Billtap](https://github.com/midagedev/billtap).

## Run

```bash
docker run --rm -p 8080:8080 ghcr.io/chelokot/stripe-emulate
```

The same command works with `podman run`. Then point your SDK at it:

```js
const stripe = new Stripe("sk_test_emulator", { host: "localhost", port: 8080, protocol: "http" });
```

`http://localhost:8080` serves the dashboard, and checkout and portal URLs open hosted pages. Add `-v stripe-emulate:/data` to keep state between runs. Without a container runtime, `go run github.com/chelokot/stripe-emulate/cmd/stripe-emulate@latest` serves the API alone, without the hosted pages.

Webhooks are signed with `Stripe-Signature`, so `stripe.webhooks.constructEvent` works unchanged. Register your app like on Stripe; from inside the container your host is `host.containers.internal` under Podman and `host.docker.internal` under Docker (on Linux add `--add-host=host.docker.internal:host-gateway`), or run with `--network host`:

```bash
curl localhost:8080/v1/webhook_endpoints -u sk_test_emulator: \
  -d url=http://host.containers.internal:3000/stripe/webhooks \
  -d secret=whsec_your_app_secret -d "enabled_events[]=*"
```

Objects accept your own ids (`-d id=prod_plus`), so seeded configs can reference them. Complete a checkout on its hosted page, or headlessly with `POST /v1/checkout/sessions/{id}/complete`.

## How close to Stripe is it

Three independent checks run in CI; each run's numbers land in the [job summary](https://github.com/chelokot/stripe-emulate/actions/workflows/ci.yml).

| Check | What it proves | Result |
| --- | --- | --- |
| **Live diff with Stripe test mode** | The same requests sent to Stripe and to the emulator return the same JSON | **41 / 41** steps identical across 6 scenarios, **1,454** fields compared |
| **Stripe's OpenAPI schemas** | Every response and webhook event the test suite produces is a valid Stripe object | **1,131** responses and **1,301** webhook events, **183** object schemas, **0** violations |
| **Official SDK** | `stripe-node` 22 drives real flows end to end, webhooks included | **21** flows |

Coverage of the 2025-12-15.clover API: **203 of 587** operations, with **100%** of the checkout, billing, customer portal, webhook endpoint and Connect families. 291 Go tests cover the behaviour behind them.

### Live diff

[`internal/parity`](internal/parity) runs scenarios for customers, checkout sessions, the billing portal, payment methods, invoices and error envelopes against both sides and compares every field:

- ids are compared by prefix (`cus_`, `cs_test_`, `il_`, …), and an id seen on one side must always map to the same id on the other, so wrong references fail;
- timestamps and URLs are compared by type; account settings such as branding are compared by shape;
- everything else, including error messages, codes and `null`s, must match exactly.

```bash
STRIPE_PARITY_KEY=sk_test_... go test -run TestParity -v ./internal/parity
```

Use a key for a dedicated test-mode account: the run creates customers, checkout sessions and invoices there. In CI the job runs when the `STRIPE_PARITY_KEY` secret is set.

### Schema check

[`internal/stripeschema`](internal/stripeschema) embeds the object schemas of Stripe's published spec for 2025-12-15.clover ([stripe/openapi@ed93b94](https://github.com/stripe/openapi/blob/ed93b94259c55782b8b04e01f4ffdaa2872e9bcd/openapi/spec3.json)). The API test suite fails on any unknown or missing field, wrong type, unexpected `null` or out-of-enum value, in responses and in every webhook event it records. The few fields live Stripe returns beyond the published spec are listed explicitly, each confirmed by the live diff. Emulator-only extensions are always prefixed `emulator_`.

### API versions

Responses follow 2025-12-15.clover. Clients pinned to a `Stripe-Version` before 2025-03-31.basil get the legacy invoice, line item and invoice item fields back (`subscription`, `paid`, `tax`, line `price`, …).

## What it emulates

- Customers, products and prices, coupons and promotion codes, tax rates
- Subscriptions with trials, prorations, schedules and test clocks
- Invoices, invoice items, credit notes, refunds and disputes
- Checkout sessions and the customer portal, with hosted pages
- Payment and setup intents; test tokens like `pm_card_visa_chargeDeclined` attach as real `pm_` ids and keep their decline behaviour
- Webhook endpoints with `Stripe-Signature` signing, retries and replay
- Connect accounts, transfers, payouts and application fees
- `expand[]` on any path

## License

Apache-2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).
