SHELL := /bin/bash
OAPI_OPERATION_IDS := createTrip,getTrip,finishTrip,health,ready
LINT_VERSION := v2.15.0
LINT_BIN ?= $(CURDIR)/bin/golangci-lint
GREMLINS_VERSION := v0.6.0
GREMLINS_BIN ?= $(CURDIR)/bin/gremlins
MUTATION_REPORT ?= docs/mutation/latest.json

.PHONY: generate migrate migrate-down migrate-status run test test-integration lint fmt build mutation

migrate:
	set -a; . ./.env.example; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down:
	set -a; . ./.env.example; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" down

migrate-status:
	set -a; . ./.env.example; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" status

run:
	set -a; . ./.env.example; . ./.env; set +a; go run ./cmd/trip-service

test:
	mkdir -p bin
	CGO_ENABLED=1 go test -race -count=2 -coverpkg=./internal/domain/...,./internal/usecase/... -coverprofile=bin/coverage.out ./...
	go tool cover -func=bin/coverage.out

test-integration: migrate
	set -a; . ./.env.example; . ./.env; set +a; CGO_ENABLED=1 go test -race -count=2 -tags=integration ./internal/repository/... ./internal/infra/postgres/...

build:
	mkdir -p bin
	go build -o bin/trip-service ./cmd/trip-service

$(LINT_BIN):
	mkdir -p $(dir $(LINT_BIN))
	GOBIN=$(abspath $(dir $(LINT_BIN))) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION)

lint: $(LINT_BIN)
	$(LINT_BIN) run ./...

fmt: $(LINT_BIN)
	$(LINT_BIN) fmt ./...

$(GREMLINS_BIN):
	mkdir -p $(dir $(GREMLINS_BIN))
	GOBIN=$(abspath $(dir $(GREMLINS_BIN))) go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)

mutation: $(GREMLINS_BIN)
	mkdir -p $(dir $(MUTATION_REPORT))
	CGO_ENABLED=1 $(GREMLINS_BIN) unleash \
		--workers 2 --test-cpu 1 --timeout-coefficient 20 --invert-logical \
		--exclude-files '(^|/)(cmd|config|container|generated|http|infra|model|repository)/' \
		--output $(MUTATION_REPORT)

generate:
	mkdir -p internal/generated
	go tool oapi-codegen \
		-generate types,chi-server \
		-include-operation-ids $(OAPI_OPERATION_IDS) \
		-package api \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml
	go generate ./internal/...
