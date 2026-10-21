FROM node:24-alpine AS web-builder

WORKDIR /src

COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig.json vite.config.ts ./
COPY web ./web
RUN npm run build

FROM golang:1.25-alpine AS go-builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/stripe-emulate ./cmd/stripe-emulate

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& addgroup -S emulator \
	&& adduser -S -D -H -G emulator emulator \
	&& mkdir -p /app/dist /data \
	&& chown -R emulator:emulator /app /data

WORKDIR /app

ENV STRIPE_EMULATE_ADDR=:8080 \
	STRIPE_EMULATE_DATABASE_URL=/data/emulator.db \
	STRIPE_EMULATE_STATIC_DIR=dist/app \
	STRIPE_EMULATE_ENV=production

COPY --from=go-builder /out/stripe-emulate /usr/local/bin/stripe-emulate
COPY --from=web-builder /src/dist/app ./dist/app

USER emulator

EXPOSE 8080
VOLUME ["/data"]
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/stripe-emulate"]
