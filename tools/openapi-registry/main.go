// Command openapi-registry derives a controls registry from OpenAPI 3
// annotations, or checks that a committed registry is up to date.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/openapidiff"
	"example.com/controls/core/internal/openapiregistry"
)

func main() {
	openapiPath := flag.String("openapi", "", "OpenAPI 3 JSON or YAML document")
	outPath := flag.String("out", "", "registry output (empty = stdout)")
	service := flag.String("service", "", "service name stamped on generated endpoints")
	check := flag.Bool("check", false, "fail if -out differs from the generated registry")
	flag.Parse()
	if *openapiPath == "" {
		fmt.Fprintln(os.Stderr, "-openapi is required")
		os.Exit(2)
	}
	doc, err := openapidiff.Load(*openapiPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	reg, err := openapiregistry.Generate(*doc, *service)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	raw, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	raw = append(raw, '\n')
	if *check {
		if *outPath == "" {
			fmt.Fprintln(os.Stderr, "-check requires -out")
			os.Exit(2)
		}
		existing, err := os.ReadFile(*outPath)
		if err != nil || !bytes.Equal(existing, raw) {
			fmt.Fprintf(os.Stderr, "%s is out of date; run openapi-registry\n", *outPath)
			os.Exit(1)
		}
		fmt.Printf("ok: %s matches %s\n", *outPath, *openapiPath)
		return
	}
	if *outPath == "" {
		_, _ = os.Stdout.Write(raw)
		return
	}
	if err := os.WriteFile(*outPath, raw, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("wrote %s\n", *outPath)
}
