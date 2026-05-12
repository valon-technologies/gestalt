FROM node:26.1.0-alpine3.23@sha256:e71ac5e964b9201072425d59d2e876359efa25dc96bb1768cb73295728d6e4ea AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.29.8-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
COPY --from=builder /app/out /usr/share/nginx/html
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
    listen 8080;
    server_name registry.gestaltd.ai;
    root /usr/share/nginx/html;
    index index.html;

    location /_next/ {
        try_files $uri =404;
    }

    location /_pagefind/ {
        try_files $uri =404;
    }

    location /favicon.svg {
        try_files $uri =404;
    }

    location /images/ {
        try_files $uri =404;
    }

    location /fonts/ {
        try_files $uri =404;
    }

    location = /404.html {
        try_files /404.html =404;
    }

    location / {
        try_files /registry.html =404;
    }

    error_page 404 /404.html;
}

server {
    listen 8080 default_server;
    server_name gestaltd.ai _;
    root /usr/share/nginx/html;
    index index.html;

    location / {
        try_files $uri $uri.html $uri/ =404;
    }

    location = /registry {
        try_files /registry.html =404;
    }

    location /registry/ {
        try_files /registry.html =404;
    }

    location ~ ^/reference/(python-sdk|sdk/python)(\.html)?/?$ {
        return 301 /api/python/index.html;
    }

    location ~ ^/reference/(typescript-sdk|sdk/typescript)(\.html)?/?$ {
        return 301 /api/typescript/index.html;
    }

    location ~ ^/reference/(go-sdk|sdk/go)(\.html)?/?$ {
        return 301 https://pkg.go.dev/github.com/valon-technologies/gestalt/sdk/go;
    }

    location ~ ^/reference/(rust-sdk|sdk/rust)(\.html)?/?$ {
        return 301 https://docs.rs/gestalt-sdk/latest/gestalt/;
    }

    error_page 404 /404.html;
}
EOF
EXPOSE 8080
