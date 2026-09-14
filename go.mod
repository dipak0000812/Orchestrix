module github.com/dipak0000812/orchestrix

go 1.24.0

toolchain go1.24.12

require (
	github.com/jackc/pgx/v5 v5.8.0
	github.com/oklog/ulid/v2 v2.1.1
	github.com/prometheus/client_golang v1.23.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

replace golang.org/x/text => github.com/golang/text v0.29.0

replace golang.org/x/sync => github.com/golang/sync v0.17.0

replace golang.org/x/sys => github.com/golang/sys v0.35.0

replace google.golang.org/protobuf => github.com/protocolbuffers/protobuf-go v1.36.8

replace gopkg.in/yaml.v3 => github.com/go-yaml/yaml/v3 v3.0.1

replace go.yaml.in/yaml/v2 => github.com/yaml/go-yaml/v2 v2.4.2

replace gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20200902074654-038fdea0a05b
