FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY v3/go.mod v3/go.sum ./
RUN go mod download
COPY v3/ ./
ARG SERVICE=gateway
ENV GOMAXPROCS=2
RUN case "$SERVICE" in gateway|control|worker|migrate) ;; *) exit 2 ;; esac && \
    CGO_ENABLED=0 go build -p 1 -trimpath -ldflags='-s -w' -o /out/server "./cmd/$SERVICE"

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S codego && adduser -S -G codego codego && \
    mkdir -p /data/wal /data/files /data/logs && chown -R codego:codego /data && chmod 700 /data/logs
COPY --from=build /out/server /app/server
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/
COPY v3/licenses/ /licenses/v3/
COPY v3/internal/commerce/invoicefonts/OFL.txt /licenses/invoice-fonts-OFL.txt
USER codego
WORKDIR /data
ENTRYPOINT ["/app/server"]
