// Command bundle renders a controls bundle for one environment/service.
//
// Jenkins usage (inside the tools container):
//
//	bundle -src . -env prod -service payments -out dist/payments
//
// Writes rules.json, mask-policies.json, registry.json, dq.json and
// manifest.json. The digest in manifest.json is what the sidecar logs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/bundle"
)

func main() {
	src := flag.String("src", "", "source directory (policies/, registry/, mask-policies/, dq/)")
	out := flag.String("out", "", "output bundle directory")
	env := flag.String("env", "", "environment overlay under env/<name>/overrides.json")
	service := flag.String("service", "", "service to render (uses services/<name>/overrides.json if present)")
	check := flag.Bool("check", false, "validate only; write nothing")
	flag.Parse()

	res, err := bundle.Render(bundle.Options{
		Src: *src, Out: *out, Env: *env, Service: *service, Check: *check,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle:", err)
		os.Exit(1)
	}
	raw, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(raw))
}
