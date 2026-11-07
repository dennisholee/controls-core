// Package contracts embeds the normative artifacts served by the tools that
// expose them. The YAML/JSON files remain the source of truth; this package
// only makes them available to a compiled binary.
package contracts

import _ "embed"

//go:embed test-api.yaml
var TestAPISpec []byte
