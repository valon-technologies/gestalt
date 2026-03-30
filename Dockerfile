ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://gestalt.run
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestaltd

FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build-binary
WORKDIR /build
COPY go.mod go.sum ./
COPY sdk/pluginapi/go.mod sdk/pluginapi/go.sum sdk/pluginapi/
COPY sdk/pluginsdk/go.mod sdk/pluginsdk/go.sum sdk/pluginsdk/
COPY examples/plugins/provider-go/go.mod examples/plugins/provider-go/go.sum examples/plugins/provider-go/
COPY examples/plugins/runtime-go/go.mod examples/plugins/runtime-go/go.sum examples/plugins/runtime-go/
RUN go mod download
COPY . .
COPY --from=frontend /web/out/ internal/webui/out/
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /gestaltd ./cmd/gestaltd
RUN mkdir /data

FROM golang:1.26-alpine AS builder
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk add --no-cache ca-certificates git
COPY --from=build-binary /gestaltd /gestaltd
RUN ln -sf /gestaltd /usr/local/bin/gestaltd
WORKDIR /src
LABEL org.opencontainers.image.title="gestaltd builder" \
      org.opencontainers.image.description="Build image for packaging plugins and bundling Gestalt deployments." \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"

FROM alpine:3.21 AS debug
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk add --no-cache ca-certificates curl
COPY --from=build-binary /gestaltd /gestaltd
RUN mkdir -p /data && chown nobody:nobody /data
USER nobody:nobody
LABEL org.opencontainers.image.title="gestaltd debug" \
      org.opencontainers.image.description="Debug image for gestaltd with a shell and troubleshooting tools." \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
EXPOSE 8080
ENTRYPOINT ["/gestaltd"]
CMD ["serve", "--locked", "--config", "/etc/gestalt/config.yaml"]

FROM gcr.io/distroless/static-debian12 AS runtime
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
COPY --from=build-binary /gestaltd /gestaltd
COPY --from=build-binary --chown=nobody:nobody /data /data
LABEL org.opencontainers.image.title="gestaltd" \
      org.opencontainers.image.description="Self-hosted integration runtime for REST, GraphQL, MCP, and plugins." \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
USER nobody:nobody
EXPOSE 8080
ENTRYPOINT ["/gestaltd"]
CMD ["serve", "--locked", "--config", "/etc/gestalt/config.yaml"]
