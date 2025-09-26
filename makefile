export PATH := $(PATH):`go env GOPATH`/bin
export GO111MODULE=on
LDFLAGS := -s -w

all: web web2api api

api:
	cd api && go mod tidy && env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/EchoArkServer main.go

web:
	cd web && npm i && npm run build

web2api:
	rm -rf api/assets/web/* && mv web/dist/* api/assets/web

clean:
	rm -rf bin/ web/dist

.PHONY: clean api web web2api