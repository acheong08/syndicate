package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
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
	log.Printf("Starting proxy at http://%s.syncthing/", protocol.NewDeviceID(cert.Certificate[0]))

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = &http.Transport{Proxy: nil}
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host
		req.URL.Path, req.URL.RawPath = joinURLPath(targetURL, req.URL)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := http.NewServeMux()
	mux.Handle("/", proxy)
	connChan := make(chan net.Conn, 5)

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

	go StartRelayManager(ctx, cert, trustedIds, connChan, relayCountry)

	log.Fatal(lib.ServeMux(ctx, mux, connChan))
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
