// Command bundle-keygen writes an Ed25519 signing key pair for bundles.
//
// Keep the private key in Jenkins credentials (or an HSM-backed signer); mount
// only the public key into the sidecar.
package main

import (
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/signing"
)

func main() {
	out := flag.String("out", ".", "directory for the key pair")
	flag.Parse()

	priv, pub, err := signing.GenerateKeyPair(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle-keygen:", err)
		os.Exit(1)
	}
	fmt.Printf("private key: %s (keep secret)\npublic key:  %s\n", priv, pub)
}
