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
COPY --from=builder /gestalt /gestalt
USER nobody:nobody
ENTRYPOINT ["/gestalt"]
