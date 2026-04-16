ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://gestaltd.ai
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestaltd

FROM --platform=$BUILDPLATFORM golang:1.26.2-alpine3.23@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS build-binary
WORKDIR /build
COPY gestaltd/go.mod gestaltd/go.sum gestaltd/
COPY sdk/go/go.mod sdk/go/go.sum sdk/go/
RUN cd gestaltd && go mod download
COPY . .
ARG TARGETARCH
ARG TARGETVARIANT
RUN mkdir -p /out && \
    cd gestaltd && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} go build -o /out/gestaltd ./cmd/gestaltd

# --- Static (default) ---
FROM scratch AS static
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
COPY --from=build-binary /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-binary /out/gestaltd /gestaltd
LABEL org.opencontainers.image.title="gestaltd" \
      org.opencontainers.image.description="Platform for self-hostable, configurable integrations and tooling" \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
EXPOSE 8080
ENTRYPOINT ["/gestaltd"]
CMD ["serve", "--locked", "--config", "/etc/gestalt/config.yaml", "--artifacts-dir", "/data"]

# --- Alpine ---
FROM alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS alpine
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates && rm -f /usr/bin/wget
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
