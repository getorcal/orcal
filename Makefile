.PHONY: build test test-docker lint generate verify-generate

build:
	go build -o dist/orcald ./cmd/orcald
	go build -o dist/orcal ./cmd/orcal

test:
	go test ./...

test-docker:
	go test -tags docker ./test/integration/...

generate:
	go generate ./...

verify-generate: generate
	git diff --exit-code internal/apigen
