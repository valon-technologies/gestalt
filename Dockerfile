FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY toolshed/go.mod toolshed/go.sum ./toolshed/
RUN cd toolshed && go mod download
COPY toolshed/ ./toolshed/
RUN cd toolshed && CGO_ENABLED=0 GOOS=linux go build -o /toolshed ./cmd/toolshed

FROM gcr.io/distroless/static-debian12
COPY --from=builder /toolshed /toolshed
EXPOSE 8080
ENTRYPOINT ["/toolshed"]
CMD ["--config", "/etc/toolshed/config.yaml"]
