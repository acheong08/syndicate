package utils_test

import (
	"crypto/rand"
	"syndicate/lib/utils"
	"testing"
)

func TestChunks(t *testing.T) {
	testCases := []struct {
		name  string
		bytes [16]byte
	}{
		{
			name: "random bytes",
		},
		{
			name:  "all zeros",
			bytes: [16]byte{},
		},
		{
			name:  "all 0xFFs",
			bytes: [16]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		},
	}

	for _, tc := range testCases {
		t.Logf("Test case %s\n", tc.name)
		t.Run(tc.name, func(t *testing.T) {
			var r [16]byte
			if n, err := rand.Read(r[:]); err != nil || n != 16 {
				t.Fatal("unable to generate random bytes")
			}

			if tc.name == "random bytes" {
				rand.Read(tc.bytes[:])
			}

			ip, port := utils.ChunkToAddress(tc.bytes[:], r)
			restored := utils.ChunkToBytes(ip, port, r)
			for i := range restored {
				if restored[i] != tc.bytes[i] {
					t.Fatalf("restored byte %d is different", i)
				}
			}
			t.Logf("IP %v, port %d", ip, port)
		})
	}
}

func TestByteToIPv6(t *testing.T) {
	var b [238]byte
	var r [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal("unable to generate random bytes")
	}
	if n, err := rand.Read(r[:]); err != nil || n != 16 {
		t.Fatal("unable to generate random bytes")
	}

	ips, ports := utils.EncodeIPv6(b[:], r)
	data := utils.DecodeIPv6(ips, ports, r)

	if len(b) != len(data) {
		t.Fatal("data length mismatch")
	}
	for i := 0; i < len(b); i++ {
		if b[i] != data[i] {
			t.Fatalf("data mismatch at index %d", i)
		}
	}
}
