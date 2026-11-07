// Command bundle-sign signs or verifies a rendered controls bundle.
//
// Sign (Jenkins, private key from credentials):
//
//	bundle-sign -bundle dist/payments -key /creds/bundle-signing.pem
//
// Verify (pipeline self-check, or the sidecar via -bundle-pubkey):
//
//	bundle-sign -bundle dist/payments -verify -pubkey /etc/controls/bundle-signing.pub.pem
package main

import (
	"flag"
	"fmt"
	"os"

	"example.com/controls/core/internal/signing"
)

func main() {
	dir := flag.String("bundle", "", "rendered bundle directory")
	key := flag.String("key", "", "private key (sign mode)")
	verify := flag.Bool("verify", false, "verify instead of sign")
	pub := flag.String("pubkey", "", "public key (verify mode)")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "bundle-sign: -bundle is required")
		os.Exit(2)
	}
	if *verify {
		if *pub == "" {
			fmt.Fprintln(os.Stderr, "bundle-sign: -pubkey is required with -verify")
			os.Exit(2)
		}
		if err := signing.VerifyDir(*dir, *pub); err != nil {
			fmt.Fprintln(os.Stderr, "bundle-sign: verify failed:", err)
			os.Exit(1)
		}
		fmt.Println("bundle signature verified")
		return
	}
	if *key == "" {
		fmt.Fprintln(os.Stderr, "bundle-sign: -key is required to sign")
		os.Exit(2)
	}
	if err := signing.SignDir(*dir, *key); err != nil {
		fmt.Fprintln(os.Stderr, "bundle-sign:", err)
		os.Exit(1)
	}
	fmt.Println("bundle signed")
}
