FROM --platform=$BUILDPLATFORM node:24-bookworm-slim@sha256:a5f4366e834695d5f363b920c104953a535d3a9864d1bdf74566a0ebe01daee5 AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/ui/dist ./internal/ui/dist
ARG TARGETOS=linux
ARG TARGETARCH
ARG VCS_REF=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X dns-assistant/internal/app.Revision=$VCS_REF" -o /out/dns-assistant ./cmd/dns-assistant

FROM build AS test
RUN go test -race ./...

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m assistant && mkdir -p /data && chown assistant:assistant /data
COPY --from=build /out/dns-assistant /usr/local/bin/dns-assistant
COPY licenses /usr/share/doc/dns-assistant/licenses
COPY THIRD_PARTY_NOTICES.md /usr/share/doc/dns-assistant/THIRD_PARTY_NOTICES.md
USER 10001:10001
WORKDIR /data
ENTRYPOINT ["dns-assistant"]
