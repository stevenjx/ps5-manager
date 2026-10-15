# 编译阶段
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o ps5-manager ./cmd/server

# 运行镜像
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app
COPY --from=builder /app/ps5-manager /app/ps5-manager

EXPOSE 8080

ENV PORT=8080
ENV PKG_DIR=/packages
ENV CONFIG_DIR=/config
ENV EXTERNAL_IP=127.0.0.1

# 元数据刮削：以下默认值均为「免 Key」数据源，无需任何 API Key 即可工作。
# IGDB_CLIENT_ID / IGDB_CLIENT_SECRET / STEAMGRIDDB_API_KEY 为可选增强，
# 请通过 .env 或 docker-compose 的 environment 注入，切勿写进镜像。
ENV METADATA_ENABLED=true
ENV HASHEOUS_API_ENABLED=true
ENV PLAYMATCH_API_ENABLED=true
ENV SWITCH_TITLEDB_ENABLED=true
ENV METADATA_LANGUAGE=zh,en
ENV METADATA_REGION=cn,us,eu,jp,wor
ENV METADATA_CONCURRENCY=4

ENTRYPOINT ["/app/ps5-manager"]
