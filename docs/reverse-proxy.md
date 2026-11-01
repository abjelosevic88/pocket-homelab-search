# Reverse proxy

Pocket Search speaks plain HTTP on one port and serves the UI and `/api/*` from the same binary. Set `BASE_URL` to the public URL and `SECURE_COOKIES=true` behind HTTPS.

## Caddy

```
search.example.com {
	reverse_proxy pocket-search:8080
}
```

## Traefik (labels)

```yaml
labels:
  - traefik.http.routers.search.rule=Host(`search.example.com`)
  - traefik.http.services.search.loadbalancer.server.port=8080
```

## nginx

```
location / {
    proxy_pass http://pocket-search:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 25m;
}
```

## Forward auth

With `AUTH_MODE=proxy` the app trusts `Remote-User` / `Remote-Email`. Exclude `/opensearch.xml` (browsers fetch it without a login), plus `/api/v1/dashboard` if a Homepage widget calls it with a bearer token.

## The camera scanner

Browsers only hand the camera to pages served over HTTPS (or `localhost`). Put the app behind TLS before expecting the barcode scanner to work on a phone.
