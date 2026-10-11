# Digests pinned for supply-chain integrity (refresh when intentionally upgrading bases).
FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
WORKDIR /src
ARG VERSION=1.7.3
ARG GIT_SHA=unknown
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
  -ldflags="-s -w -X github.com/Kyaxris-Labs/Noctaxris-GCP/internal/version.Version=${VERSION}" \
  -o /noctaxris-gcp ./cmd/noctaxris-gcp
RUN mkdir -p /var/lib/noctaxris-gcp && chown 65532:65532 /var/lib/noctaxris-gcp

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35
ARG VERSION=1.7.3
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
