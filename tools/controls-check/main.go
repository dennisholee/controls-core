// Command controls-check cross-checks a rendered bundle against the OpenAPI
// contract before it is signed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/controlscheck"
	"example.com/controls/core/internal/openapidiff"
)

func main() {
	openapiPath := flag.String("openapi", "", "OpenAPI 3 JSON or YAML document")
	bundleDir := flag.String("bundle", "", "rendered bundle directory")
	pipFixture := flag.String("pip-fixture", "", "optional PIP fixture JSON")
	flag.Parse()
	if *openapiPath == "" || *bundleDir == "" {
		fmt.Fprintln(os.Stderr, "-openapi and -bundle are required")
		os.Exit(2)
	}
	doc, err := openapidiff.Load(*openapiPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	bundle, err := config.LoadDir(*bundleDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var fixture map[string]any
	if *pipFixture != "" {
		raw, err := os.ReadFile(*pipFixture)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := json.Unmarshal(raw, &fixture); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	res := controlscheck.Validate(bundle, doc, fixture)
	for _, w := range res.Warnings {
		fmt.Println("warning:", w)
	}
	for _, e := range res.Errors {
		fmt.Println("error:", e)
	}
	if len(res.Errors) > 0 {
		os.Exit(1)
	}
	fmt.Printf("ok: bundle matches %s\n", *openapiPath)
}
