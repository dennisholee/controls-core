// Command conformance executes fixtures against a rendered bundle.
//
// Jenkins usage:
//
//	conformance -bundle dist/payments -fixtures tests/fixtures -v
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/conformance"
)

func main() {
	bundleDir := flag.String("bundle", "", "rendered bundle directory")
	fixtures := flag.String("fixtures", "tests/fixtures", "fixture directory")
	service := flag.String("service", "", "only run fixtures for this service")
	verbose := flag.Bool("v", false, "print each case")
	flag.Parse()

	if *bundleDir == "" {
		fmt.Fprintln(os.Stderr, "conformance: -bundle is required")
		os.Exit(2)
	}
	res, err := conformance.Run(*bundleDir, *fixtures, *service, *verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, "conformance:", err)
		os.Exit(1)
	}
	raw, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(raw))
	if res.Failed > 0 {
		os.Exit(1)
	}
}
