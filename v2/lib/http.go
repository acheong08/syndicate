package lib

import (
	"context"
	"errors"
	"net"
	"net/http"
)

var ErrListenerClosed = errors.New("listener closed")

func ServeMux(ctx context.Context, mux *http.ServeMux, connChan <-chan net.Conn) error {
	listener := ConnListener{connChan, make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			listener.Close()
		}
	}()
	return http.Serve(listener, mux)
}

type ConnListener struct {
	connCh <-chan net.Conn
	closed chan struct{}
}

func (l ConnListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case <-l.closed:
		return nil, ErrListenerClosed
	}
}

func (l ConnListener) Close() error {
	close(l.closed)
	return nil
}

func (l ConnListener) Addr() net.Addr {
	return &net.TCPAddr{} // or a custom Addr
}
