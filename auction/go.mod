module github.com/chew01/ixp-gcp/auction

go 1.25.4

require (
	github.com/atomix/go-sdk v0.10.0
	github.com/chew01/ixp-gcp/proto v0.0.0-20260119172851-9849c54bc75b
	github.com/chew01/ixp-gcp/shared v0.0.0
	google.golang.org/grpc v1.76.0
)

require (
	github.com/atomix/runtime/api v0.7.0 // indirect
	github.com/atomix/runtime/sdk v0.7.2 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/net v0.46.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251222181119-0a764e51fe1b // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
)

replace github.com/chew01/ixp-gcp/shared => ../shared
