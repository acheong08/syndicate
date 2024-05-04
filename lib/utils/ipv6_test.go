package utils_test

import (
	"crypto/rand"
	"syndicate/lib/utils"
	"testing"
)

func TestByteToIPv6(t *testing.T) {
	var b [16]byte
	var r [16]byte
	if n, err := rand.Read(b[:]); err != nil || n != 16 {
		t.Fatal("unable to generate random bytes")
	}
	if n, err := rand.Read(r[:]); err != nil || n != 16 {
		t.Fatal("unable to generate random bytes")
	}

	ip, port, err := utils.BytesToAddress(b[:], r)
	if err != nil {
		t.Fatal(err)
	}
	restored := utils.AddressToBytes(ip, port, r)
	for i := range restored {
		if restored[i] != b[i] {
			t.Fatalf("restored byte %d is different", i)
		}
	}
	t.Logf("IP %v, port %d", ip, port)
}
