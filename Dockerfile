# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:22-bookworm-slim
ARG GO_IMAGE=golang:1.26-bookworm
ARG RUNTIME_IMAGE=alpine/git:latest
ARG PNPM_VERSION=11.9.0
ARG PNPM_REGISTRY=https://registry.npmjs.org

FROM ${NODE_IMAGE} AS frontend
ARG PNPM_VERSION
ARG PNPM_REGISTRY
WORKDIR /src
RUN corepack enable && corepack prepare "pnpm@${PNPM_VERSION}" --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json web/package.json
RUN --mount=type=cache,target=/pnpm/store \
    pnpm config set store-dir /pnpm/store && \
    pnpm config set registry "${PNPM_REGISTRY}" && \
    pnpm config set fetch-retries 5 && \
    pnpm config set fetch-retry-maxtimeout 120000 && \
    pnpm config set fetch-timeout 600000 && \
    pnpm install --filter @gohermit/web... --frozen-lockfile
COPY web web
RUN pnpm build

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY protocol protocol
RUN rm -rf internal/web/assets/dist
COPY --from=frontend /src/internal/web/assets/dist internal/web/assets/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hermit ./cmd/hermit && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hermit-web ./cmd/hermit-web

FROM ${RUNTIME_IMAGE} AS runtime
USER root
RUN mkdir -p /data && chown 501:20 /data
COPY --from=build /out/hermit /usr/local/bin/hermit
COPY --from=build /out/hermit-web /usr/local/bin/hermit-web
ENV HOME=/tmp/gohermit GOCACHE=/tmp/go-cache
WORKDIR /workspace
EXPOSE 8787
ENTRYPOINT ["hermit-web"]
CMD ["-listen", "0.0.0.0:8787", "-workspace", "/workspace", "-config", "/config/hermit.toml"]
