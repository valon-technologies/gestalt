FROM nginx:1.31.4-alpine@sha256:db35bfc6b2951e7f8a72db5db120288c127ffaeeb4a6d4b95a26fead017d5913
COPY out /usr/share/nginx/html
# The machine-consumed provider index is published with the registry so
# `gestaltd provider repo add` has a consumer-neutral URL. Baked at image
# build from the same source the registry catalog is baked from at site
# build, so the browsable registry and the index deploy as one snapshot.
# --chmod: ADD from a URL defaults to 0600, which nginx's worker (user
# nginx) cannot read — it serves 403 instead of the index.
ADD --chmod=644 https://raw.githubusercontent.com/valon-technologies/gestalt-providers/main/provider-index.yaml /usr/share/nginx/html/provider-index.yaml
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

    location = /provider-index.yaml {
        types { }
        default_type application/yaml;
        try_files /provider-index.yaml =404;
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

    # The registry's canonical home is its own origin; the old apex mount
    # redirects there path-for-path so existing links keep working.
    location = /registry {
        return 301 https://registry.gestaltd.ai/$is_args$args;
    }

    location ~ ^/registry/(.*)$ {
        return 301 https://registry.gestaltd.ai/$1$is_args$args;
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
