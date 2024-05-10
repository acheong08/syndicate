package utils_test

import (
	"crypto/rand"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"
	"testing"
)

const DEVICE_ID_LENGTH = utils.DEVICE_ID_LENGTH

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
			var r [DEVICE_ID_LENGTH]byte
			if n, err := rand.Read(r[:]); err != nil || n != DEVICE_ID_LENGTH {
				t.Fatal("unable to generate random bytes")
			}

			if tc.name == "random bytes" {
				rand.Read(tc.bytes[:])
			}

			ip, port, err := utils.ChunkToAddress(tc.bytes[:], r)
			if err != nil {
				t.Fatal(err)
			}
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
	for i := 0; i < 10000; i++ {
		var b [238]byte
		var r [DEVICE_ID_LENGTH]byte
		if _, err := rand.Read(b[:]); err != nil {
			t.Fatal("unable to generate random bytes")
		}
		if n, err := rand.Read(r[:]); err != nil || n != DEVICE_ID_LENGTH {
			t.Fatal("unable to generate random bytes")
		}

		ips, ports, err := utils.EncodeIPv6(b[:], r)
		if err != nil {
			t.Fatal(err)
		}
		data, err := utils.DecodeIPv6(ips, ports, r)
		if err != nil {
			t.Fatal(err)
		}

		if len(b) != len(data) {
			t.Fatal("data length mismatch")
		}
		for i := 0; i < len(b); i++ {
			if b[i] != data[i] {
				t.Fatalf("data mismatch at index %d", i)
			}
		}
	}
}

func TestWithZeroRand(t *testing.T) {
	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic")
		}
	}()
	var b [238]byte
	var r [DEVICE_ID_LENGTH]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal("unable to generate random bytes")
	}

	utils.EncodeIPv6(b[:], r)
}

func TestSmallerThan16(t *testing.T) {
	var b [2]byte
	var r [DEVICE_ID_LENGTH]byte
	rand.Read(b[:])
	rand.Read(r[:])
	ips, ports, _ := utils.EncodeIPv6(b[:], r)
	data, _ := utils.DecodeIPv6(ips, ports, r)
	if len(data) != 2 {
		t.Fatalf("data length mismatch. got %d, expected 2", len(data))
	}
	for i := 0; i < len(b); i++ {
		if b[i] != data[i] {
			t.Fatalf("data mismatch at index %d", i)
		}
	}
}
