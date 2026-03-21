FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /gestalt-server ./cmd/gestalt-server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /gestalt-server /gestalt-server
EXPOSE 8080
ENTRYPOINT ["/gestalt-server"]
CMD ["--config", "/etc/gestalt/config.yaml"]
