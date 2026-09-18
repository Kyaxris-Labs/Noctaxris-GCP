package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/tlsutil"
)

func main() {
	out := "./lab-ca"
	if len(os.Args) > 1 && os.Args[1] != "" {
		out = os.Args[1]
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lab ca path: %v\n", err)
		os.Exit(1)
	}
	paths, err := tlsutil.EnsureServerCert(abs, time.Time{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate lab ca: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("CA cert:     %s\n", paths.CACert)
	fmt.Printf("Server cert: %s\n", paths.ServerCert)
	fmt.Printf("Server key:  %s\n", paths.ServerKey)
	fmt.Println("Do not commit private keys. Trust lab-ca.crt only on lab hosts.")
}
