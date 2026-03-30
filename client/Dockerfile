FROM rust:1-alpine AS builder
RUN apk add --no-cache musl-dev perl make
WORKDIR /build
COPY . .
RUN cargo build --release

FROM gcr.io/distroless/static-debian12
COPY --from=builder /build/target/release/gestalt /gestalt
USER nonroot:nonroot
ENTRYPOINT ["/gestalt"]
