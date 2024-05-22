// Upgrade connections to TLS
package utils

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"log"
	"net"

	"github.com/rotisserie/eris"
)

func UpgradeClientConn(conn net.Conn, cert tls.Certificate) (net.Conn, error) {
	tlsConfig := tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	tlsConn := tls.Client(conn, &tlsConfig)
	err := tlsConn.Handshake()
	if err != nil {
		return nil, eris.Wrap(err, "Could not complete TLS handshake")
	}
	log.Println("Waiting for magic")
	if err := magic(tlsConn); err != nil {
		return nil, eris.Wrap(err, "Magic handshake failed")
	}
	log.Println("Magic success")
	return tlsConn, nil
}

func UpgradeServerConn(conn net.Conn, cert tls.Certificate, clientCert *x509.Certificate) (net.Conn, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	if clientCert != nil {
		clientCertPool := x509.NewCertPool()
		clientCertPool.AddCert(clientCert)
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    clientCertPool,
		}
	}
	var err error
	tlsConn := tls.Server(conn, tlsConfig)
	if err = tlsConn.Handshake(); err != nil {
		return nil, eris.Wrap(err, "Could not complete TLS handshake")
	}
	log.Println("TLS handshake completed")
	// We read before writing to prevent EOF to client
	if err = magic(tlsConn); err != nil {
		return nil, eris.Wrap(err, "Magic handshake failed")
	}
	log.Println("Magic succeeded")
	return tlsConn, nil
}

func magic(conn net.Conn) error {
	// Do this a few times just to make sure
	for i := 0; i < 3; i++ {
		if err := writeMagic(conn); err != nil {
			return eris.Wrap(err, "Could not write magic")
		}
		if err := readMagic(conn); err != nil {
			return eris.Wrap(err, "Could not read magic")
		}
	}
	return nil
}

func writeMagic(conn net.Conn) error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, 0xdeadface)
	_, err := conn.Write(buf)
	return err
}

func readMagic(conn net.Conn) error {
	buf := make([]byte, 8)
	_, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if binary.LittleEndian.Uint64(buf) != 0xdeadface {
		return eris.New("invalid magic number")
	}
	return nil
}
