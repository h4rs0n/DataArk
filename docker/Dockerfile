FROM node:20 AS frontend-builder

COPY web /DataArk/web
COPY makefile /DataArk/makefile
WORKDIR /DataArk
RUN make web

FROM golang:1.23 AS backend-builder

COPY api /DataArk/api
COPY makefile /DataArk/makefile
WORKDIR /DataArk
COPY --from=frontend-builder /DataArk/web/dist /DataArk/api/assets/web
RUN make api

FROM alpine:3

RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai

COPY --from=backend-builder /DataArk/bin/EchoArkServer /usr/bin/EchoArkServer
EXPOSE 7845
ENTRYPOINT ["/usr/bin/EchoArkServer"]