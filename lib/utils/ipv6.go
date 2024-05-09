package utils

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log"
	"math"
	"net"

	"github.com/syncthing/syncthing/lib/protocol"
)

const DEVICE_ID_LENGTH = protocol.DeviceIDLength

var (
	ErrInvalidMagic  = errors.New("invalid magic number")
	ErrNoSafeAddress = errors.New("failed to generate a safe address")
)

func EncodeIPv6(b []byte, r [DEVICE_ID_LENGTH]byte) (ips []net.IP, ports []uint16, err error) {
	// If r is all zeros, let the last byte be 1
	if r == [DEVICE_ID_LENGTH]byte{} {
		panic("invalid random bytes")
	}
	size := int(math.Ceil(float64(len(b)+4) / 16))

	// Pre-allocate the ips slice (len(b) + 4) / 16
	ips = make([]net.IP, size)
	ports = make([]uint16, size)
	var chunk [16]byte
	// Magic 2-byte number to identify the data
	binary.BigEndian.PutUint16(chunk[:], 0xdead)
	// Take the next 2 bytes to encode the length of the data
	binary.BigEndian.PutUint16(chunk[2:], uint16(len(b)))

	// Fill out b such that we have at least 16 bytes
	if len(b) < 16 && len(b)%16 != 0 {
		randomBytes := make([]byte, 16-len(b)%16)
		rand.Read(randomBytes)
		b = append(b, randomBytes...)
		if len(b) != 16 {
			panic("ur dumb")
		}
	}

	// Then take 12 bytes from the data to fill in the rest of the chunk
	copy(chunk[4:], b[:12])

	ips[0], ports[0], err = ChunkToAddress(chunk[:], r)
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
		ips[i], ports[i], err = ChunkToAddress(chunk[:], r)
	}
	return
}

func DecodeIPv6(ips []net.IP, ports []uint16, r [DEVICE_ID_LENGTH]byte) ([]byte, error) {
	// If r is all zeros, let the last byte be 1
	if r == [DEVICE_ID_LENGTH]byte{} {
		panic("invalid random bytes")
	}
	// Get the length of the full data by decoding the first chunk
	chunk := ChunkToBytes(ips[0], ports[0], r)
	magic := binary.BigEndian.Uint16(chunk[:2])
	if magic != 0xdead {
		return nil, ErrInvalidMagic
	}
	length := binary.BigEndian.Uint16(chunk[2:4])
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
	return data, nil
}

func ChunkToAddress(b []byte, r [DEVICE_ID_LENGTH]byte) (ip net.IP, port uint16, err error) {
	if len(b) != 16 {
		panic("invalid length")
	}
	// Run it through xor at least once to ensure randomness
	ip = xorIpv6(append([]byte{}, b...), r)
	ip, port, err = toSafeAddress(ip, r)
	return
}

func ChunkToBytes(ip net.IP, port uint16, r [DEVICE_ID_LENGTH]byte) []byte {
	// Copy the IP to avoid modifying the original slice
	ip = append([]byte{}, ip...)
	for i := 0; i < len(r); i += 2 {
		port ^= binary.BigEndian.Uint16(r[i:])
	}
	for i := uint16(0); i < port; i++ {
		ip = xorIpv6(ip, r)
	}
	return ip
}

func xorIpv6(ip net.IP, r [DEVICE_ID_LENGTH]byte) net.IP {
	// We do this inefficient mess in case any significant random bytes are 0
	for i := range ip {
		ip[i] ^= r[i]
		ip[i] ^= r[i*2]
	}
	return ip
}

func toSafeAddress(ip net.IP, r [DEVICE_ID_LENGTH]byte) (newIp net.IP, port uint16, err error) {
	newIp = ip
	for {
		if newIp.IsLoopback() || newIp.IsUnspecified() {
			newIp = xorIpv6(ip, r)
			port++
			if port > 1000 {
				log.Println(r, ip)
				err = ErrNoSafeAddress
				return
			}
		} else {
			break
		}
	}
	for i := 0; i < len(r); i += 2 {
		port ^= binary.BigEndian.Uint16(r[i:])
	}
	// Increment one more time to avoid 0 as the port
	port++
	return
}
