// Command openapi-diff enforces the registry ↔ OpenAPI gate.
//
// Usage:
//
//	openapi-diff -openapi openapi.json -registry registry.json
//
// Both inputs are JSON. Convert a YAML OpenAPI document to JSON in the delivery
// pipeline (for example with `yq -o=json`).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/openapidiff"
)

func main() {
	openapiPath := flag.String("openapi", "", "OpenAPI 3 JSON document")
	registryPath := flag.String("registry", "", "rendered registry JSON")
	flag.Parse()

	if *openapiPath == "" || *registryPath == "" {
		fmt.Fprintln(os.Stderr, "both -openapi and -registry are required")
		os.Exit(2)
	}

	var doc openapidiff.Document
	readJSON(*openapiPath, &doc)

	var reg config.Registry
	readJSON(*registryPath, &reg)

	endpoints := make([]openapidiff.Endpoint, 0, len(reg.Endpoints))
	for _, ep := range reg.Endpoints {
		endpoints = append(endpoints, openapidiff.Endpoint{ID: ep.ID, Method: ep.Method, Path: ep.Path})
	}

	res := openapidiff.Check(doc, endpoints)
	failed := false
	if len(res.MissingRegistry) > 0 {
		failed = true
		fmt.Println("documented operations with no registry entry:")
		for _, m := range res.MissingRegistry {
			fmt.Println("  - " + m)
		}
	}
	if len(res.OrphanRegistry) > 0 {
		failed = true
		fmt.Println("registry entries with no documented operation:")
		for _, o := range res.OrphanRegistry {
			fmt.Println("  - " + o)
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("ok: %d registry endpoints match the OpenAPI document\n", len(endpoints))
}

func readJSON(path string, v any) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(2)
	}
	if err := json.Unmarshal(data, v); err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", path, err)
		os.Exit(2)
	}
}
