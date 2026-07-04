module github.com/ai8future/airborne

go 1.26.2

require (
	github.com/ai8future/chassis-go-addons/pgkit v1.2.3
	github.com/ai8future/chassis-go-addons/rediskit v1.2.3
	github.com/ai8future/chassis-go-addons/ssrfcheck v1.2.3
	github.com/ai8future/chassis-go/v11 v11.1.8
	github.com/ai8future/markdown_svc/clients/go v0.0.0
	github.com/ai8future/pricing_db v0.0.0-00010101000000-000000000000
	github.com/alicebob/miniredis/v2 v2.35.0
	github.com/anthropics/anthropic-sdk-go v1.19.0
	github.com/fatih/color v1.18.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/openai/openai-go v1.12.0
	github.com/redis/go-redis/v9 v9.17.2
	github.com/spf13/cobra v1.10.2
	golang.org/x/crypto v0.53.0
	google.golang.org/genai v1.40.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/ai8future/chassis-go/v11 => ../../chassis_suite/chassis-go

replace github.com/ai8future/markdown_svc/clients/go => ./markdown_svc/clients/go

// Use shared pricing_db library
replace github.com/ai8future/pricing_db => ../pricing_db

replace github.com/ai8future/chassis-go-addons/rediskit => ../../chassis_suite/chassis-go-addons/rediskit

replace github.com/ai8future/chassis-go-addons/pgkit => ../../chassis_suite/chassis-go-addons/pgkit

replace github.com/ai8future/chassis-go-addons/ssrfcheck => ../../chassis_suite/chassis-go-addons/ssrfcheck

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.7 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/twmb/franz-go v1.20.7 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.12.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
)
