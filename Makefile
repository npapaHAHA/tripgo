OAPI_OPERATION_IDS := createTrip,getTrip,finishTrip,health,ready

.PHONY: generate migrate migrate-down migrate-status run test

migrate:
	set -a; . ./.env.example; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down:
	set -a; . ./.env.example; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" down

migrate-status:
	set -a; . ./.env.example; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" status

run:
	set -a; . ./.env.example; . ./.env; set +a; go run ./cmd/trip-service

test:
	go test ./...

generate:
	mkdir -p internal/generated
	go tool oapi-codegen \
		-generate types,chi-server \
		-include-operation-ids $(OAPI_OPERATION_IDS) \
		-package api \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml
