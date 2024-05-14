// Upgrade connections to TLS
package utils

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"time"
)

func UpgradeClientConn(conn net.Conn, cert tls.Certificate, timeout time.Duration) (net.Conn, error) {
	return doWithTimeout(func(res chan net.Conn, errChan chan error) {
		conn, err := upgradeClientConn(conn, cert)
		if err != nil {
			errChan <- err
		} else {
			res <- conn
		}
	}, timeout)
}

func upgradeClientConn(conn net.Conn, cert tls.Certificate) (net.Conn, error) {
	tlsConfig := tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	tlsConn := tls.Client(conn, &tlsConfig)
	err := tlsConn.Handshake()
	if err != nil {
		return nil, err
	}
	if err := magic(tlsConn, false); err != nil {
		return nil, err
	}
	return tlsConn, err
}

func doWithTimeout(fn func(chan net.Conn, chan error), timeout time.Duration) (net.Conn, error) {
	res := make(chan net.Conn, 1)
	errChan := make(chan error, 1)
	go fn(res, errChan)
	select {
	case conn := <-res:
		return conn, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(timeout):
		return nil, errors.New("timeout")
	}
}

func UpgradeServerConn(conn net.Conn, cert tls.Certificate, clientCert *x509.Certificate, timeout time.Duration) (net.Conn, error) {
	return doWithTimeout(func(res chan net.Conn, errChan chan error) {
		conn, err := upgradeServerConn(conn, cert, clientCert)
		if err != nil {
			errChan <- err
		} else {
			res <- conn
		}
	}, timeout)
}

func upgradeServerConn(conn net.Conn, cert tls.Certificate, clientCert *x509.Certificate) (net.Conn, error) {
	clientCertPool := x509.NewCertPool()
	clientCertPool.AddCert(clientCert)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCertPool,
	}
	var err error
	tlsConn := tls.Server(conn, tlsConfig)
	if err = tlsConn.Handshake(); err != nil {
		return nil, err
	}
	log.Println("TLS handshake completed")
	// We read before writing to prevent EOF to client
	if err = magic(tlsConn, true); err != nil {
		return nil, err
	}
	log.Println("Magic succeeded")
	return tlsConn, err
}

func magic(conn net.Conn, readFirst bool) error {
	if readFirst {
		if err := readMagic(conn); err != nil {
			return err
		}
		if err := writeMagic(conn); err != nil {
			return err
		}
	} else {
		if err := writeMagic(conn); err != nil {
			return err
		}
		if err := readMagic(conn); err != nil {
			return err
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
		return errors.New("invalid magic number")
	}
	return nil
}
