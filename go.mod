module example.com/controls/core

go 1.26.0

require (
	github.com/cedar-policy/cedar-go v1.9.0
	github.com/envoyproxy/go-control-plane/envoy v1.40.0
	google.golang.org/grpc v1.85.0
)

require (
	github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	golang.org/x/exp v0.0.0-20220921023135-46d9e7742f1e // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Cedar imports golang.org/x/exp; use the upstream GitHub mirror so builds do
// not depend on the golang.org vanity endpoint.
replace golang.org/x/exp => github.com/golang/exp v0.0.0-20220921023135-46d9e7742f1e
