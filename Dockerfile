FROM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/out/ internal/webui/out/
RUN CGO_ENABLED=0 GOOS=linux go build -o /gestaltd ./cmd/gestaltd

FROM gcr.io/distroless/static-debian12
COPY --from=builder /gestaltd /gestaltd
EXPOSE 8080
ENTRYPOINT ["/gestaltd"]
CMD ["--config", "/etc/gestalt/config.yaml"]
