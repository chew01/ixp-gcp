module github.com/chew01/ixp-gcp/dummy

go 1.25.4

require (
	github.com/chew01/ixp-gcp/proto v0.0.0-20260202154331-40072f19eb55
	github.com/chew01/ixp-gcp/shared v0.0.0
	github.com/segmentio/kafka-go v0.4.50
	google.golang.org/grpc v1.78.0
)

require (
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260202165425-ce8ad4cf556b // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/chew01/ixp-gcp/shared => ../shared
