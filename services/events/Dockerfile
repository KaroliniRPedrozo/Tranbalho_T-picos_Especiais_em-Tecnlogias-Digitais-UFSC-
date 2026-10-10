# ---------- build ----------
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Camada de dependencias separada: so refaz o download quando go.mod/go.sum mudam.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# CGO desabilitado -> binario estatico, roda em imagem scratch/alpine sem libc.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${APP_VERSION:-0.1.0}" \
    -o /out/events-api ./cmd/api

# ---------- runtime ----------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl \
    && adduser -D -u 10001 appuser

COPY --from=builder /out/events-api /usr/local/bin/events-api

USER appuser
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/events-api"]
