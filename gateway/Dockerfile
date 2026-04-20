## ============================================================
## LLM Logger Gateway - 多阶段构建 (静态二进制)
## ============================================================

## ------- Stage 1: builder -------
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# 利用 layer cache: 先拷贝 go.mod 拉依赖
COPY go.mod go.sum* ./
RUN go mod download

# 拷贝源码
COPY . .

# 构建
ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT}" \
    -o /out/gateway \
    ./cmd/gateway

## ------- Stage 2: runtime -------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/gateway /app/gateway

ENV TZ=UTC

EXPOSE 8080 9090

USER nonroot:nonroot
ENTRYPOINT ["/app/gateway"]
