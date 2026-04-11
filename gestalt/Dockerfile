ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://gestaltd.ai
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestalt

FROM --platform=$BUILDPLATFORM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS unpack
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

FROM gcr.io/distroless/cc-debian12@sha256:847433844c7e04bcf07a3a0f0f5a8de554c6df6fa9e3e3ab14d3f6b73d780235 AS distroless
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
