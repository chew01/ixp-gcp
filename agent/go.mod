module github.com/chew01/ixp-gcp/agent

go 1.25.4

require github.com/chew01/ixp-gcp/shared v0.0.0

require (
	github.com/goccy/go-yaml v1.19.2 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/chew01/ixp-gcp/shared => ../shared
