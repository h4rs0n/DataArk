FROM golang:1.23 AS building

COPY . /building
WORKDIR /building

RUN make build

FROM alpine:3

RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai

COPY --from=building /building/bin/EchoArkServer /usr/bin/EchoArkServer

EXPOSE 7845

ENTRYPOINT ["/usr/bin/EchoArkServer"]