# stripe-emulate

Local Stripe API emulator. Point any Stripe SDK at it and run billing flows offline, deterministically, with signed webhooks.

Not affiliated with Stripe. Fork of [Billtap](https://github.com/midagedev/billtap).

## Run

```bash
npm ci && npm run build
go run ./cmd/stripe-emulate
```

```js
const stripe = new Stripe("sk_test_emulator", { host: "localhost", port: 8080, protocol: "http" });
```

Or with Docker:

```bash
docker build -t stripe-emulate . && docker run -p 8080:8080 stripe-emulate
```

## License

Apache-2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).
