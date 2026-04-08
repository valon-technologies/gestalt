ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://gestaltd.ai
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestaltd

FROM --platform=$BUILDPLATFORM node:25.9.0-alpine3.23@sha256:ad82ecad30371c43f4057aaa4800a8ed88f9446553a2d21323710c7b937177fc AS frontend
WORKDIR /ui
COPY gestaltd/ui/package.json gestaltd/ui/package-lock.json ./
RUN npm ci
COPY gestaltd/ui/ .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.2-alpine3.23@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS build-binary
WORKDIR /build
COPY gestaltd/go.mod gestaltd/go.sum gestaltd/
COPY sdk/go/go.mod sdk/go/go.sum sdk/go/
RUN cd gestaltd && go mod download
COPY . .
COPY --from=frontend /ui/out/ gestaltd/internal/webui/out/
ARG TARGETARCH
RUN mkdir -p /out && \
    cd gestaltd && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /out/gestaltd ./cmd/gestaltd

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=build-binary /out/ /usr/local/bin/
RUN mkdir -p /data && chown nobody:nobody /data
LABEL org.opencontainers.image.title="gestaltd" \
      org.opencontainers.image.description="Platform for self-hostable, configurable integrations and tooling" \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
USER nobody:nobody
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gestaltd"]
CMD ["serve", "--locked", "--config", "/etc/gestalt/config.yaml", "--artifacts-dir", "/data"]
