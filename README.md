# stripe-emulate

Local Stripe API emulator. Point any Stripe SDK at it and run billing flows offline, deterministically, with signed webhooks.

Not affiliated with Stripe. Fork of [Billtap](https://github.com/midagedev/billtap).

## Run

```bash
docker run --rm -p 8080:8080 ghcr.io/chelokot/stripe-emulate
```

```js
const stripe = new Stripe("sk_test_emulator", { host: "localhost", port: 8080, protocol: "http" });
```

Without Docker, `go run github.com/chelokot/stripe-emulate/cmd/stripe-emulate@latest` serves the API alone, without the hosted checkout, portal and dashboard pages. From a checkout: `npm ci && npm run build && go run ./cmd/stripe-emulate`.

## Parity

`internal/parity` sends the same requests to Stripe test mode and to the emulator and diffs the responses:

```bash
STRIPE_PARITY_KEY=sk_test_... go test -run TestParity -v ./internal/parity
```

Use a key for a dedicated test-mode account: the run creates customers, checkout sessions and invoices there.

Beyond what a key can exercise, the test suite checks every API response and webhook event against Stripe's published object schemas for 2025-12-15.clover (`internal/stripeschema`).

## License

Apache-2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).
