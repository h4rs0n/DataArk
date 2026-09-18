FROM node:24 AS frontend-builder

COPY web/package.json web/package-lock.json /DataArk/web/
WORKDIR /DataArk/web
RUN npm ci
COPY web/env.d.ts web/index.html web/tsconfig*.json web/vite.config.ts /DataArk/web/
COPY web/public /DataArk/web/public
COPY web/src /DataArk/web/src
RUN npm run build

# 后端：模块下载与编译分层，并用 BuildKit cache mount 跨构建复用依赖
FROM golang:1.26 AS backend-builder

# GOPROXY/GOSUMDB 走国内源；GOTOOLCHAIN=local 禁止再下一份 Go 工具链
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn \
    GOTOOLCHAIN=local \
    CGO_ENABLED=0

# 只拷模块清单，go.mod/go.sum 不变时这一层可复用
COPY api/go.mod api/go.sum /DataArk/api/
WORKDIR /DataArk/api
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 源码和前端产物放在 download 之后，避免改 Vue/Go 时打废模块层
COPY api /DataArk/api
COPY --from=frontend-builder /DataArk/web/dist /DataArk/api/assets/web

# 不跑 go mod tidy；模块缓存在 /go/pkg/mod，编译缓存在 GOCACHE
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /DataArk/bin && \
    go build -trimpath -ldflags "-s -w" -o /DataArk/bin/EchoArkServer .

FROM alpine:3

RUN apk add --no-cache tzdata postgresql-client
ENV TZ=Asia/Shanghai

COPY --from=backend-builder /DataArk/bin/EchoArkServer /usr/bin/EchoArkServer
EXPOSE 7845
ENTRYPOINT ["/usr/bin/EchoArkServer"]
