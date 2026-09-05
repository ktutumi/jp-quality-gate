GOBIN ?= $(HOME)/.local/bin

.PHONY: build install test vet test-integrations check clean

build:
	mkdir -p ./bin
	go build -o ./bin/jp-quality-gate ./cmd/jp-quality-gate
	go build -o ./bin/jpqg-build-unihan ./cmd/jpqg-build-unihan

install:
	mkdir -p "$(GOBIN)"
	GOBIN="$(GOBIN)" go install ./cmd/jp-quality-gate
	GOBIN="$(GOBIN)" go install ./cmd/jpqg-build-unihan

test:
	go test ./...

vet:
	go vet ./...

test-integrations:
	bun test integrations/omp/index.test.js
	node --test integrations/pi/index.test.js

check: test vet test-integrations

clean:
	rm -rf ./bin
