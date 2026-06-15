ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://gestaltd.ai
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestalt

FROM --platform=$BUILDPLATFORM alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4 AS unpack
ARG TARGETARCH
COPY dist/gestalt-linux-*.tar.gz /dist/
RUN case "$TARGETARCH" in \
      amd64) archive=/dist/gestalt-linux-x86_64.tar.gz ;; \
      arm64) archive=/dist/gestalt-linux-arm64.tar.gz ;; \
      arm)   archive=/dist/gestalt-linux-armv7.tar.gz ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    mkdir -p /out && \
    tar -xzf "$archive" -C /out && \
    test -x /out/gestalt

FROM gcr.io/distroless/cc-debian12@sha256:aa0b7af67fa8211751ea6e00baa8373ba56cc1417ffc986ec9619bd0e1556b56 AS distroless
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
COPY --from=unpack /out/gestalt /gestalt
LABEL org.opencontainers.image.title="gestalt" \
      org.opencontainers.image.description="CLI client for managing Gestalt integrations, authentication, and operations." \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
USER nonroot:nonroot
ENTRYPOINT ["/gestalt"]
