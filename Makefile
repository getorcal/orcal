.PHONY: build test test-docker lint generate verify-generate up down logs

build:
	go build -o bin/orcald ./cmd/orcald
	go build -o bin/orcal ./cmd/orcal

test:
	go test ./...

test-docker:
	go test -tags docker ./test/integration/...

generate:
	go generate ./...

verify-generate: generate
	git diff --exit-code internal/apigen

up:
	docker compose -f deploy/docker-compose.yml up -d --build

down:
	docker compose -f deploy/docker-compose.yml down

logs:
	docker compose -f deploy/docker-compose.yml logs -f orcald
