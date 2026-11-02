# syntax=docker/dockerfile:1

# The trmnl-poller service. A builder with the Go toolchain and a
# shell-less static runtime that runs as nonroot (65532).

# ── builder ──────────────────────────────────────────────────────────────────
FROM cgr.dev/chainguard/go:latest AS builder

# The builder never ships. Root makes its writes independent of the base
# image's default user.
USER root
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy only what the Go build needs. There is no .dockerignore for this
# image, so a plain COPY . . would also send .git and the Python tree.
COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG VERSION=dev

# CGO_ENABLED=0: the runtime image has no libc. The binary embeds the IANA
# time zone database (time/tzdata), because the runtime image has none.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/trmnl-poller ./cmd/trmnl-poller

# ── runtime ──────────────────────────────────────────────────────────────────
FROM cgr.dev/chainguard/static:latest AS runtime

LABEL org.opencontainers.image.source="https://github.com/evandhoffman/trmnl-pws"

COPY --from=builder /out/trmnl-poller /usr/local/bin/trmnl-poller

USER nonroot
EXPOSE 8790

# Exec form: the runtime image has no shell, and the binary is PID 1
# and gets SIGTERM.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/trmnl-poller", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/trmnl-poller"]
CMD ["serve"]
