# Multi-stage Dockerfile for Bifrost Gateway + ChronoBrief freshcache plugin

# ==========================================
# STAGE 1: Build bifrost-http
# ==========================================
FROM golang:1.27.3-alpine3.24 AS bifrost-builder

RUN apk add --no-cache git gcc musl-dev bash binutils make

# Clone bifrost pinned by commit SHA corresponding to transports/v2.4.0 (F5)
ARG BIFROST_COMMIT=e869c3f9adc96ea53c38694a21bb04a8e1085487
RUN git -c advice.detachedHead=false clone https://github.com/maximhq/bifrost /build/bifrost \
    && cd /build/bifrost \
    && git checkout ${BIFROST_COMMIT}

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

# Create non-root bifrost user and group, and create needed directories (F3)
RUN addgroup -S bifrost && adduser -S -G bifrost bifrost \
    && mkdir -p /etc/bifrost/plugins /var/log/bifrost

# Copy bifrost binary and plugin
COPY --from=bifrost-builder /app/bifrost-http /usr/local/bin/bifrost-http
COPY --from=plugin-builder /app/plugins/freshcache.so /etc/bifrost/plugins/freshcache.so

# Copy gateway config and entrypoint
COPY gateway/config.json /etc/bifrost/config.json
COPY gateway/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh && chmod +x /usr/local/bin/docker-entrypoint.sh \
    && chown -R bifrost:bifrost /etc/bifrost /var/log/bifrost

USER bifrost

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["-app-dir", "/etc/bifrost", "-host", "0.0.0.0", "-port", "8080"]
