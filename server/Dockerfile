ARG GESTALT_VERSION=dev
ARG GESTALT_REVISION=unknown
ARG GESTALT_CREATED=unknown
ARG GESTALT_SOURCE=https://github.com/valon-technologies/gestalt
ARG GESTALT_DOCUMENTATION=https://docs.valon.tools
ARG GESTALT_URL=https://hub.docker.com/r/valontechnologies/gestaltd

FROM --platform=$BUILDPLATFORM node:20-alpine@sha256:f598378b5240225e6beab68fa9f356db1fb8efe55173e6d4d8153113bb8f333c AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS build-binary
WORKDIR /build
COPY server/go.mod server/go.sum server/
COPY sdk/go/go.mod sdk/go/go.sum sdk/go/
RUN cd server && go mod download
COPY . .
COPY --from=frontend /web/out/ server/internal/webui/out/
ARG TARGETARCH
RUN cd server && CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /gestaltd ./cmd/gestaltd
RUN mkdir /data

FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
ARG GESTALT_VERSION
ARG GESTALT_REVISION
ARG GESTALT_CREATED
ARG GESTALT_SOURCE
ARG GESTALT_DOCUMENTATION
ARG GESTALT_URL
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=build-binary /gestaltd /gestaltd
RUN mkdir -p /data && chown nobody:nobody /data
LABEL org.opencontainers.image.title="gestaltd" \
      org.opencontainers.image.description="Self-hosted integration server for REST, GraphQL, MCP, and plugins." \
      org.opencontainers.image.source="${GESTALT_SOURCE}" \
      org.opencontainers.image.documentation="${GESTALT_DOCUMENTATION}" \
      org.opencontainers.image.url="${GESTALT_URL}" \
      org.opencontainers.image.version="${GESTALT_VERSION}" \
      org.opencontainers.image.revision="${GESTALT_REVISION}" \
      org.opencontainers.image.created="${GESTALT_CREATED}"
USER nobody:nobody
EXPOSE 8080
ENTRYPOINT ["/gestaltd"]
CMD ["serve", "--locked", "--config", "/etc/gestalt/config.yaml"]
