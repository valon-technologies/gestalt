FROM rust:1-alpine AS builder
RUN apk add --no-cache musl-dev perl make
WORKDIR /build
COPY client/ .
RUN cargo build --release

FROM gcr.io/distroless/static-debian12
COPY --from=builder /build/target/release/gestalt /gestalt
ENTRYPOINT ["/gestalt"]
