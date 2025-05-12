package config

import (
	"crypto/tls"
)

type KeyPair struct {
	Key  []byte
	Cert []byte
}

func (k KeyPair) Certificate() (tls.Certificate, error) {
	return tls.X509KeyPair(k.Cert, k.Key)
}
