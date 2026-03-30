ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://docs.toolshed.peachstreet.dev
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestalt

FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.9.0 AS xx

FROM --platform=$BUILDPLATFORM rust:1-alpine AS builder
COPY --from=xx / /
RUN apk add --no-cache perl make
ARG TARGETPLATFORM
RUN xx-apk add --no-cache musl-dev gcc
WORKDIR /build
COPY . .
RUN RUST_TARGET=$(xx-info rust-target) && \
    LINKER=$(xx-info triple)-gcc && \
    rustup target add "$RUST_TARGET" && \
    mkdir -p .cargo && \
    printf '[target.%s]\nlinker = "%s"\n' "$RUST_TARGET" "$LINKER" > .cargo/config.toml && \
    CC="$LINKER" cargo build --release --target "$RUST_TARGET" && \
    cp "target/$RUST_TARGET/release/gestalt" /gestalt && \
    xx-verify /gestalt

FROM gcr.io/distroless/static-debian12
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
COPY --from=builder /gestalt /gestalt
LABEL org.opencontainers.image.title="gestalt" \
      org.opencontainers.image.description="CLI client for managing Gestalt integrations, authentication, and operations." \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
USER nobody:nobody
ENTRYPOINT ["/gestalt"]
