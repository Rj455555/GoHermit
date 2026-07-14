ARG GO_IMAGE=golang:1.26-bookworm
FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hermit ./cmd/hermit && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hermit-web ./cmd/hermit-web

FROM ${GO_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git && rm -rf /var/lib/apt/lists/* && \
    mkdir -p /data && chown 501:20 /data
COPY --from=build /out/hermit /usr/local/bin/hermit
COPY --from=build /out/hermit-web /usr/local/bin/hermit-web
ENV HOME=/tmp/gohermit GOCACHE=/tmp/go-cache
WORKDIR /workspace
EXPOSE 8787
ENTRYPOINT ["hermit-web"]
CMD ["-listen", "0.0.0.0:8787", "-workspace", "/workspace", "-config", "/config/hermit.toml"]
