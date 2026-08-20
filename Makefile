.PHONY: build test test-docker lint fmt generate generate-python generate-ts verify-generate up down logs

build:
	go build -o bin/orcald ./cmd/orcald
	go build -o bin/orcal ./cmd/orcal

test:
	go test ./...

test-docker:
	go test -tags docker ./test/integration/...

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

generate-python:
	cd sdk/python && python -m datamodel_code_generator \
		--input ../../spec/openapi.yaml \
		--input-file-type openapi \
		--output src/orcal/models.py \
		--output-model-type dataclasses.dataclass \
		--target-python-version 3.10 \
		--use-standard-collections \
		--use-union-operator \
		--disable-timestamp

generate-ts:
	cd sdk/typescript && npx --yes openapi-typescript ../../spec/openapi.yaml -o src/models.ts

generate: generate-python generate-ts
	go generate ./...

verify-generate: generate
	git diff --exit-code internal/apigen sdk/python/src/orcal/models.py sdk/typescript/src/models.ts

up:
	docker compose -f deploy/docker-compose.yml up -d --build

down:
	docker compose -f deploy/docker-compose.yml down

logs:
	docker compose -f deploy/docker-compose.yml logs -f orcald
