FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /toolshed ./cmd/toolshed

FROM gcr.io/distroless/static-debian12
COPY --from=builder /toolshed /toolshed
EXPOSE 8080
ENTRYPOINT ["/toolshed"]
CMD ["--config", "/etc/toolshed/config.yaml"]
