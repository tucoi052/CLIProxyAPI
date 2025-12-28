FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=prod
ARG COMMIT=none
ARG BUILD_DATE

# Set BUILD_DATE to current UTC time if not provided, then build
RUN BUILD_DATE=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)} && \
    CGO_ENABLED=0 GOOS=linux go build \
        -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" \
        -o ./CLIProxyAPI ./cmd/server/

FROM alpine:3.22.0

RUN apk add --no-cache tzdata

RUN mkdir /CLIProxyAPI

COPY --from=builder ./app/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI

COPY config.example.yaml /CLIProxyAPI/config.example.yaml
COPY static/antigravity-quota.html /CLIProxyAPI/static/antigravity-quota.html
RUN chmod 644 /CLIProxyAPI/static/antigravity-quota.html

WORKDIR /CLIProxyAPI

EXPOSE 8317

ENV TZ=Asia/Ho_Chi_Minh

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

CMD ["./CLIProxyAPI"]