# E2E probe replay omitted the idempotency key

The production-image E2E probe sent a stable request ID but left `idempotency_key` empty, so its second call could redispatch the provider instead of exercising the keyed replay path. The probe now requires an explicit idempotency key, maps it separately from the request ID, and both replay invocations pass the same deterministic key. The stale Go package evidence was also regenerated to include `internal/service/proto_contract_test.go`.
