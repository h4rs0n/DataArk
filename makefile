export PATH := $(PATH):`go env GOPATH`/bin
export GO111MODULE=on
LDFLAGS := -s -w

all: web build

build:
	cd api && go mod tidy && env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/EchoArkServer main.go

web:
	cd web && npm i && npm run build && rm -rf ../api/assets/web/* && mv dist/* ../api/assets/web

clean:
	rm -rf bin/ web/dist

.PHONY: clean build web
