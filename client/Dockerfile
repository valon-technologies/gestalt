ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://docs.valon.tools
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestalt

FROM --platform=$BUILDPLATFORM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS unpack
ARG TARGETARCH
WORKDIR /dist
COPY dist/ /dist/
RUN case "$TARGETARCH" in \
      amd64) archive=gestalt-linux-x86_64.tar.gz ;; \
      arm64) archive=gestalt-linux-arm64.tar.gz ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    mkdir -p /out && \
    tar -xzf "/dist/$archive" -C /out && \
    test -x /out/gestalt

FROM gcr.io/distroless/cc-debian12@sha256:329e54034ce498f9c6b345044e8f530c6691f99e94a92446f68c0adf9baa8464
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
