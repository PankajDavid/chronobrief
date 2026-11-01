# Multi-stage Dockerfile for Bifrost Gateway + ChronoBrief freshcache plugin

# ==========================================
# STAGE 1: Build bifrost-http
# ==========================================
FROM golang:1.27.3-alpine3.24 AS bifrost-builder

RUN apk add --no-cache git gcc musl-dev bash binutils make

# Clone bifrost at pinned transports/v2.4.0 tag
RUN git clone --depth 1 --branch transports/v2.4.0 https://github.com/maximhq/bifrost /build/bifrost

WORKDIR /build/bifrost/transports/bifrost-http
# Create minimal ui directory for //go:embed all:ui
RUN mkdir -p ui && touch ui/index.html
RUN CGO_ENABLED=1 go build -tags sqlite_static -o /app/bifrost-http .

# ==========================================
# STAGE 2: Build freshcache.so plugin
# ==========================================
FROM golang:1.27.3-alpine3.24 AS plugin-builder

RUN apk add --no-cache gcc musl-dev bash binutils

WORKDIR /build/freshcache
COPY gateway/plugins/freshcache/go.mod gateway/plugins/freshcache/go.sum ./
RUN go mod download

COPY gateway/plugins/freshcache/ ./
RUN CGO_ENABLED=1 go build -buildmode=plugin -o /app/plugins/freshcache.so main.go

# Verify all 7 symbols
COPY scripts/check_plugin_symbols.sh /build/check_plugin_symbols.sh
RUN chmod +x /build/check_plugin_symbols.sh && /build/check_plugin_symbols.sh /app/plugins/freshcache.so

# ==========================================
# STAGE 3: Runtime
# ==========================================
FROM alpine:3.24 AS runtime

RUN apk add --no-cache ca-certificates tzdata curl libc6-compat

# Create directories for config, plugins, and audit logs
RUN mkdir -p /etc/bifrost/plugins /var/log/bifrost

# Copy bifrost binary and plugin
COPY --from=bifrost-builder /app/bifrost-http /usr/local/bin/bifrost-http
COPY --from=plugin-builder /app/plugins/freshcache.so /etc/bifrost/plugins/freshcache.so

# Copy gateway config
COPY gateway/config.json /etc/bifrost/config.json

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/bifrost-http"]
CMD ["-app-dir", "/etc/bifrost", "-host", "0.0.0.0", "-port", "8080"]
