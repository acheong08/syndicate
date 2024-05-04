package utils

import (
	"errors"
	"net"
)

const ErrTooMuchData = "unable to encode this much data"

func BytesToAddress(b []byte, r [16]byte) (ip net.IP, port int, err error) {
	ip, err = bytesToIPv6(b)
	if err != nil {
		return
	}
	ip, port = toSafeAddress(ip, r)
	return
}

func AddressToBytes(ip net.IP, port int, rand [16]byte) []byte {
	for i := 0; i < port-1; i++ {
		ip = xorIpv6(ip, rand)
	}
	return ip
}

func bytesToIPv6(b []byte) (net.IP, error) {
	if len(b) > 16 {
		return nil, errors.New(ErrTooMuchData)
	}
	if len(b) < 16 {
		b = append(make([]byte, 16-len(b)), b...)
	}
	return net.IP(b), nil
}

func xorIpv6(ip net.IP, rand [16]byte) net.IP {
	for i := range ip {
		ip[i] ^= rand[i]
	}
	return ip
}

func toSafeAddress(ip net.IP, rand [16]byte) (newIp net.IP, port int) {
	newIp = ip
	for {
		if !newIp.IsGlobalUnicast() {
			newIp = xorIpv6(ip, rand)
			port++
		} else {
			break
		}
	}
	// Increment one more time to avoid 0 as the port
	port++
	return
}
