# Digests pinned for supply-chain integrity (refresh when intentionally upgrading bases).
FROM golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS build
WORKDIR /src
ARG VERSION=1.0.1
ARG GIT_SHA=unknown
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
  -ldflags="-s -w -X github.com/Kyaxris-Labs/Noctaxris-GCP/internal/version.Version=${VERSION}" \
  -o /noctaxris-gcp ./cmd/noctaxris-gcp
RUN mkdir -p /var/lib/noctaxris-gcp && chown 65532:65532 /var/lib/noctaxris-gcp

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=1.0.1
ARG GIT_SHA=unknown
LABEL org.opencontainers.image.title="Noctaxris-GCP" \
  org.opencontainers.image.description="GCP-shaped local security lab emulator" \
  org.opencontainers.image.source="https://github.com/Kyaxris-Labs/Noctaxris-GCP" \
  org.opencontainers.image.version="${VERSION}" \
  org.opencontainers.image.revision="${GIT_SHA}"
USER nonroot:nonroot
COPY --from=build /noctaxris-gcp /noctaxris-gcp
COPY --from=build --chown=65532:65532 /var/lib/noctaxris-gcp /var/lib/noctaxris-gcp
EXPOSE 4588
ENTRYPOINT ["/noctaxris-gcp"]
