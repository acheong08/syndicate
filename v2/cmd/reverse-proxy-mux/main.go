package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/mux"
	"github.com/syncthing/syncthing/lib/protocol"
)

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	apath := a.EscapedPath()
	if apath == "" {
		apath = "/"
	}
	bpath := b.EscapedPath()
	if strings.HasSuffix(apath, "/") && strings.HasPrefix(bpath, "/") {
		return apath + bpath[1:], apath + bpath[1:]
	}
	return apath + bpath, apath + bpath
}

func main() {
	target := flag.String("target", "", "target URL for reverse proxy (required)")
	keysPath := flag.String("keys", "", "Path to gob encoded KeyPair")
	trustedIdsPath := flag.String("trusted", "", "Path to newline separated by newlines")
	country := flag.String("country", "", "Country code for relay selection (auto-detect if empty)")
	flag.Parse()

	if *target == "" {
		log.Fatal("The --target flag is required")
	}

	targetURL, err := url.Parse(*target)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}

	var cert tls.Certificate
	if keysPath == nil || *keysPath == "" {
		cert, err = crypto.NewCertificate("syncthing-server", 1)
	} else {
		cert, err = internal.ReadKeyPair(*keysPath)
	}
	if err != nil {
		panic(err)
	}

	trustedIds, err := LoadTrustedDeviceIDs(*trustedIdsPath)
	if err != nil {
		panic(err)
	}

	log.Printf("Starting multiplexed reverse proxy at http://%s.syncthing/", protocol.NewDeviceID(cert.Certificate[0]))

	// Create reverse proxy with multiplexed backend connections
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host
		req.URL.Path, req.URL.RawPath = joinURLPath(targetURL, req.URL)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var relayCountry string
	if *country != "" {
		relayCountry = *country
	} else {
		relayCountry, err = detectCountry()
		if err != nil {
			log.Printf("Could not auto-detect country, defaulting to 'DE': %v", err)
			relayCountry = "DE"
		}
	}

	// Start relay manager
	relayChan := lib.StartRelayManager(ctx, cert, trustedIds, relayCountry)

	// Handle incoming relay connections with multiplexing
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case relayOut := <-relayChan:
				go handleMultiplexedReverseProxy(ctx, relayOut.Conn, proxy)
			}
		}
	}()

	select {}
}

// handleMultiplexedReverseProxy handles a single relay connection with multiplexing for reverse proxy
func handleMultiplexedReverseProxy(ctx context.Context, conn net.Conn, proxy *httputil.ReverseProxy) {
	defer conn.Close()

	// Create server session for multiplexing
	session := mux.NewServerSession(ctx, conn)
	defer session.Close()

	log.Printf("New multiplexed reverse proxy connection from %s", conn.RemoteAddr())

	// Accept streams and handle each as an HTTP connection
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Printf("Failed to accept stream: %v", err)
			return
		}

		// Handle each stream as a separate HTTP connection
		go func(s net.Conn) {
			defer s.Close()
			if err := http.Serve(&singleConnListener{conn: s}, proxy); err != nil {
				log.Printf("HTTP serve error: %v", err)
			}
		}(stream)
	}
}

// singleConnListener is a net.Listener that returns a single connection once
type singleConnListener struct {
	conn net.Conn
	used bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	if l.used {
		// Block forever after first use
		select {}
	}
	l.used = true
	return l.conn, nil
}

func (l *singleConnListener) Close() error {
	return l.conn.Close()
}

func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

func detectCountry() (string, error) {
	type ipinfo struct {
		Country string `json:"country"`
	}
	resp, err := http.Get("https://ipinfo.io/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var info ipinfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.Country == "" {
		return "", nil
	}
	return info.Country, nil
}

func LoadTrustedDeviceIDs(path string) ([]protocol.DeviceID, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	trustedIds := []protocol.DeviceID{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 64 {
			line = line[:64]
		}
		id, err := protocol.DeviceIDFromString(line)
		if err != nil {
			return nil, err
		}
		trustedIds = append(trustedIds, id)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return trustedIds, nil
}
