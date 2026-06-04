FROM golang:1.22-alpine AS builder

WORKDIR /app

# 先拷贝依赖文件，利用 Docker 缓存层加速
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# ---- 运行时镜像 ----
FROM alpine:3.19

RUN apk add --no-cache tzdata ca-certificates

COPY --from=builder /app/server /server

EXPOSE 8080

CMD ["/server"]
