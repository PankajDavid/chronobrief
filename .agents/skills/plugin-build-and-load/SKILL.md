---
name: plugin-build-and-load
description: Builds the Bifrost gateway and the freshcache .so in one Docker build so toolchain and core versions match, proves the plugin loaded, and diagnoses load failures. Use when the plugin does not load, after changing Go or Bifrost versions, or when writing the Dockerfile and compose file.
---

# Build and load the plugin

Why Docker: the plugin must match the gateway's Go toolchain and package versions exactly and cannot be cross-compiled, so build both in the same image, from the same checkout, in the same command. On Windows, work inside WSL2 and keep the repo on the Linux filesystem.

## Dockerfile pattern (verify every path and flag against docs/BIFROST_RECON.md before using)
```dockerfile
# Stage 1: build gateway and plugin together
FROM golang:<GO_VERSION_FROM_RECON> AS build
ARG BIFROST_TAG=<TAG_FROM_RECON>
ENV CGO_ENABLED=1
RUN git clone --depth 1 --branch ${BIFROST_TAG} https://github.com/maximhq/bifrost /src/bifrost
# build the gateway (path from recon, typically under transports/bifrost-http)
WORKDIR /src/bifrost/<GATEWAY_PATH>
RUN go build -o /out/bifrost-http .
# build the plugin pinned to the exact core version the gateway uses
WORKDIR /src/plugin
COPY gateway/plugins/freshcache/ .
COPY scripts/check_plugin_symbols.sh /src/check_plugin_symbols.sh
RUN CORE_VER=$(cd /src/bifrost/<GATEWAY_PATH> && go list -m -f '{{.Version}}' github.com/maximhq/bifrost/core) \
 && go get github.com/maximhq/bifrost/core@${CORE_VER} && go mod tidy \
 && go build -buildmode=plugin -o /out/freshcache.so .
RUN go version -m /out/bifrost-http | grep -E 'bifrost/core|^/out' ; go version -m /out/freshcache.so | grep -E 'bifrost/core|^/out'
RUN sh /src/check_plugin_symbols.sh /out/freshcache.so

# Stage 2: run on the same base the binaries were built on (glibc must match)
FROM golang:<GO_VERSION_FROM_RECON>
COPY --from=build /out/ /app/
```
Notes: runtime image must share the build image's libc; do not switch to alpine or distroless without rebuilding both binaries there. Run as a non-root user in the final image.

## Compose pattern
Services: `bifrost` (mounts `gateway/config.json`, the plugin directory, and `./data/audit` read-write) and `app` (mounts `./data/audit` read-only, reads `BIFROST_URL=http://bifrost:8080`). Bifrost's provider key comes from `.env` via environment, not baked into config.

## Symbol check
`scripts/check_plugin_symbols.sh` runs `go tool nm` on the .so and fails unless all seven hook symbols are present. A missing symbol means a typo that Bifrost would silently ignore.

## Load failure table (from the Bifrost docs; re-verify)
| Symptom | Cause | Fix |
|---|---|---|
| `plugin was built with a different version of package ...` | Go or any shared dependency differs | compare `go version -m` of .so and gateway; rebuild from the same stage; align `core` version |
| `plugin: not a plugin file` | not built with `-buildmode=plugin` | use the build command above |
| `cannot load plugin built for GOOS=linux on darwin` | built on the wrong OS | build inside the Linux container |
| `symbol Init not found` | wrong export name or signature | exports start with a capital; compare to recon signatures |
| hook never fires, no error | misspelt hook name | run the symbol check |
| plugin loads, but no HTTP hooks log | gateway not in HTTP transport mode | check recon answer 9 |

## Proof of load
`docker compose up --build`, then one `curl` to `/v1/chat/completions`; paste the plugin's log lines into `docs/evidence/plugin-load.txt`. Time-box: 90 minutes, then ADR-002 fallback.
