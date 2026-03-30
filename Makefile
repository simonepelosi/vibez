GOFLAGS=-tags webkit2gtk_4_1
PKG_CONFIG_PATH=$(CURDIR)/pkg-config

.PHONY: build run test lint clean

build:
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build -o vibez .

run: build
	./vibez

test:
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go test ./...

lint:
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) golangci-lint run

clean:
	rm -f vibez
