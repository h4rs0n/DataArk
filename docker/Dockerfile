FROM node:24 AS frontend-builder

COPY web/package.json web/package-lock.json /DataArk/web/
WORKDIR /DataArk/web
RUN npm ci
COPY web/env.d.ts web/index.html web/tsconfig*.json web/vite.config.ts /DataArk/web/
COPY web/public /DataArk/web/public
COPY web/src /DataArk/web/src
RUN npm run build

FROM golang:1.26 AS backend-builder

COPY api /DataArk/api
COPY makefile /DataArk/makefile
WORKDIR /DataArk
COPY --from=frontend-builder /DataArk/web/dist /DataArk/api/assets/web
RUN make api

FROM alpine:3

RUN apk add --no-cache tzdata postgresql-client
ENV TZ=Asia/Shanghai

COPY --from=backend-builder /DataArk/bin/EchoArkServer /usr/bin/EchoArkServer
EXPOSE 7845
ENTRYPOINT ["/usr/bin/EchoArkServer"]
