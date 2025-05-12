package internal

import (
	"crypto/tls"
	"encoding/gob"
	"os"

	"github.com/acheong08/syndicate/v2/lib/config"
)

func ReadKeyPair(path string) (tls.Certificate, error) {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	var kp config.KeyPair
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&kp); err != nil {
		panic(err)
	}
	return tls.X509KeyPair(kp.Cert, kp.Key)
}
