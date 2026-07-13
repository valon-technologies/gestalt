ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://gestaltd.ai
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestaltd

FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS build-binary
WORKDIR /build
COPY gestaltd/go.mod gestaltd/go.sum gestaltd/
COPY gestaltd/rpc/go.mod gestaltd/rpc/go.sum gestaltd/rpc/
COPY sdk/go/go.mod sdk/go/go.sum sdk/go/
RUN cd gestaltd && go mod download
COPY . .
ARG GESTALT_VERSION
ARG TARGETARCH
ARG TARGETVARIANT
RUN mkdir -p /out && \
    cd gestaltd && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -ldflags "-X main.version=${GESTALT_VERSION}" -o /out/gestaltd ./cmd/gestaltd

FROM --platform=$BUILDPLATFORM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS prebuilt-binary
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates && rm -f /usr/bin/wget
COPY dist/ /dist/
ARG TARGETARCH
ARG TARGETVARIANT
RUN set -eux; \
    mkdir -p /out; \
    case "${TARGETARCH}/${TARGETVARIANT}" in \
      amd64/) suffix="linux-x86_64" ;; \
      arm64/) suffix="linux-arm64" ;; \
      arm/v7) suffix="linux-armv7" ;; \
      *) echo "unsupported target platform: ${TARGETARCH}/${TARGETVARIANT}" >&2; exit 1 ;; \
    esac; \
    tar -xzf "/dist/gestaltd-${suffix}.tar.gz" -C /out gestaltd

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
CMD ["serve", "--config", "/etc/gestaltd/config.yaml", "--artifacts-dir", "/data"]

FROM scratch AS static-prebuilt
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
COPY --from=prebuilt-binary /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=prebuilt-binary /out/gestaltd /gestaltd
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
CMD ["serve", "--config", "/etc/gestaltd/config.yaml", "--artifacts-dir", "/data"]

# --- Alpine ---
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS alpine
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates libgcc libstdc++ && rm -f /usr/bin/wget
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
CMD ["serve", "--config", "/etc/gestaltd/config.yaml", "--artifacts-dir", "/data"]

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS alpine-prebuilt
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates libgcc libstdc++ && rm -f /usr/bin/wget
COPY --from=prebuilt-binary /out/gestaltd /usr/local/bin/gestaltd
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
CMD ["serve", "--config", "/etc/gestaltd/config.yaml", "--artifacts-dir", "/data"]
