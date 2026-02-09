module github.com/chew01/ixp-gcp/dummy

go 1.25.4

require (
	github.com/chew01/ixp-gcp/shared v0.0.0
	github.com/segmentio/kafka-go v0.4.50
)

require (
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	golang.org/x/net v0.49.0 // indirect
)

replace github.com/chew01/ixp-gcp/shared => ../shared
