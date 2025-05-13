package main

import (
	"encoding/gob"
	"encoding/pem"
	"flag"
	"fmt"
	"os"

	"github.com/acheong08/syndicate/v2/lib/config"
	"github.com/acheong08/syndicate/v2/lib/crypto"
)

func main() {
	expiryDays := flag.Int("expiry", 1095, "Days until the certificate should expire")
	outputPath := flag.String("output", "", "Output file path (required)")
	flag.Parse()
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "--output is required")
		os.Exit(1)
	}
	cert, key, err := crypto.GenerateCertificate("syncthing", *expiryDays)
	if err != nil {
		panic(err)
	}
	kp := config.KeyPair{Key: pem.EncodeToMemory(key), Cert: pem.EncodeToMemory(cert)}
	// Sanity check
	_, err = kp.Certificate()
	if err != nil {
		panic(err)
	}
	f, err := os.Create(*outputPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	if err := enc.Encode(&kp); err != nil {
		panic(err)
	}
}
