FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/go-sqlite-blog .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 blog \
    && adduser -S -D -H -u 10001 -G blog blog \
    && mkdir -p /app/static/uploads /data \
    && chown -R blog:blog /app /data

WORKDIR /app

COPY --from=build --chown=blog:blog /out/go-sqlite-blog ./go-sqlite-blog
COPY --from=build --chown=blog:blog /src/tables.sql ./tables.sql
COPY --from=build --chown=blog:blog /src/static/templates ./static/templates

ENV PORT=8080 \
    DATABASE_SOURCE=/data/blog.db \
    LOG_LEVEL=INFO

EXPOSE 8080
USER blog

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["./go-sqlite-blog"]
