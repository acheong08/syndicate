package utils

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"net"
)

func EncodeIPv6(b []byte, r [16]byte) (ips []net.IP, ports []uint16) {
	size := int(math.Ceil(float64(len(b)+4) / 16))
	// Pre-allocate the ips slice (len(b) + 4) / 16
	ips = make([]net.IP, size)
	ports = make([]uint16, size)
	var chunk [16]byte
	// Take the first 4 bytes encode the length of the full data
	binary.BigEndian.PutUint32(chunk[:], uint32(len(b)))

	// Then take 12 bytes from the data to fill in the rest of the chunk
	copy(chunk[4:], b[:12])

	ips[0], ports[0] = ChunkToAddress(chunk[:], r)
	// Then for each 16 byte chunk, encode it into an address
	n := 12
	for i := 1; i < size; i++ {
		var chunk [16]byte
		if n+16 < len(b) {
			copy(chunk[:], b[n:n+16])
			n += 16
		} else {
			copy(chunk[:], b[n:])
			// Fill in the rest with random bytes
			rand.Read(chunk[len(b)-n:])
		}
		ips[i], ports[i] = ChunkToAddress(chunk[:], r)
	}
	return
}

func DecodeIPv6(ips []net.IP, ports []uint16, r [16]byte) []byte {
	// Get the length of the full data by decoding the first chunk
	chunk := ChunkToBytes(ips[0], ports[0], r)
	length := binary.BigEndian.Uint32(chunk[:4])
	// Pre-allocate the data slice
	data := make([]byte, length)
	// Copy the first 12 bytes from the first chunk
	copy(data[:], chunk[4:])
	n := 12
	// Then for each chunk, decode it into the data slice
	for i := 1; i < len(ips); i++ {
		chunk = ChunkToBytes(ips[i], ports[i], r)
		if n+16 < len(data) {
			copy(data[n:], chunk[:])
		} else {
			copy(data[n:], chunk[:len(data)-n])
		}
		n += 16
	}
	return data
}

func ChunkToAddress(b []byte, r [16]byte) (ip net.IP, port uint16) {
	ip = bytesToIPv6(b)
	// Run it through xor at least once to ensure randomness
	ip = xorIpv6(ip, r)
	ip, port = toSafeAddress(ip, r)
	return
}

func ChunkToBytes(ip net.IP, port uint16, rand [16]byte) []byte {
	// Copy the IP to avoid modifying the original slice
	ip = append([]byte{}, ip...)
	for i := 0; i < len(rand); i += 2 {
		port ^= binary.BigEndian.Uint16(rand[i:])
	}
	for i := uint16(0); i < port; i++ {
		ip = xorIpv6(ip, rand)
	}
	return ip
}

func bytesToIPv6(b []byte) net.IP {
	if len(b) != 16 {
		panic("invalid length")
	}
	// Copy the bytes to a new slice to avoid the slice pointing to the original slice
	return net.IP(append([]byte{}, b...))
}

func xorIpv6(ip net.IP, rand [16]byte) net.IP {
	// We do this inefficient mess in case any significant random bytes are 0
	for i := range ip {
		for j := range rand {
			ip[i] ^= rand[j]
		}
	}
	return ip
}

func toSafeAddress(ip net.IP, rand [16]byte) (newIp net.IP, port uint16) {
	newIp = ip
	for {
		if !newIp.IsGlobalUnicast() {
			newIp = xorIpv6(ip, rand)
			port++
			if port > 100 {
				panic("unable to find a address, did you forget to initialize random?")
			}
		} else {
			break
		}
	}
	for i := 0; i < len(rand); i += 2 {
		port ^= binary.BigEndian.Uint16(rand[i:])
	}
	// Increment one more time to avoid 0 as the port
	port++
	return
}
