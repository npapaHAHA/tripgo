OAPI_OPERATION_IDS := createTrip,getTrip,finishTrip,health,ready

.PHONY: generate
generate:
	mkdir -p internal/generated
	go tool oapi-codegen \
		-generate types,chi-server \
		-include-operation-ids $(OAPI_OPERATION_IDS) \
		-package api \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml
