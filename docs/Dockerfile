FROM nginx:1.31.1-alpine@sha256:8b1e78743a03dbb2c95171cc58639fef29abc8816598e27fb910ed2e621e589a
COPY out /usr/share/nginx/html
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
    listen 8080;
    server_name registry.gestaltd.ai;
    absolute_redirect off;
    port_in_redirect off;
    root /usr/share/nginx/html;
    index index.html;
    add_header Cache-Control "no-cache, max-age=0, must-revalidate" always;

    location /_next/ {
        add_header Cache-Control "public, max-age=31536000, immutable" always;
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
    absolute_redirect off;
    port_in_redirect off;
    root /usr/share/nginx/html;
    index index.html;
    add_header Cache-Control "no-cache, max-age=0, must-revalidate" always;

    location = / {
        return 301 /latest/;
    }

    location = /latest {
        return 301 /latest/;
    }

    location = /dev {
        return 301 /dev/;
    }

    location ~ ^(/versions/v[^/]+)$ {
        return 301 $1/;
    }

    location ~ ^/(dev|latest|versions/v[^/]+)(/api/.*)$ {
        return 301 $2;
    }

    location ~ ^(/(dev|latest)/.+)/$ {
        return 301 $1;
    }

    location ~ ^(/versions/v[^/]+/.+)/$ {
        return 301 $1;
    }

    location ~ ^/(dev|latest|versions/v[^/]+)/_next/ {
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location /dev/ {
        try_files $uri $uri.html $uri/ =404;
    }

    location /latest/ {
        try_files $uri $uri.html $uri/ =404;
    }

    location /versions/ {
        try_files $uri $uri.html $uri/ =404;
    }

    location = /registry {
        try_files /registry.html =404;
    }

    location /registry/ {
        try_files /registry.html =404;
    }

    location ^~ /api/ {
        try_files $uri $uri.html $uri/ =404;
    }

    location = /versions.json {
        try_files /versions.json =404;
    }

    location = /404.html {
        try_files /404.html =404;
    }

    location = /install.sh {
        try_files /install.sh =404;
    }

    location = /install-gestaltd.sh {
        try_files /install-gestaltd.sh =404;
    }

    location = /install-gestalt.sh {
        try_files /install-gestalt.sh =404;
    }

    location /_next/ {
        add_header Cache-Control "public, max-age=31536000, immutable" always;
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

    location ~ ^/providers/[^/]+/.+ {
        return 404;
    }

    location / {
        return 301 /latest$request_uri;
    }

    error_page 404 /404.html;
}
EOF
EXPOSE 8080
