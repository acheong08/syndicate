package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/discovery"
	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/syncthing/syncthing/lib/protocol"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
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
	flag.Parse()
	// Handling command line arguments
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
	trustedIds := []protocol.DeviceID{}
	if *trustedIdsPath != "" {
		f, err := os.Open(*trustedIdsPath)
		if err != nil {
			panic(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if len(line) > 64 {
				line = line[:64]
			}
			id, err := protocol.DeviceIDFromString(line)
			if err != nil {
				panic(err)
			}
			trustedIds = append(trustedIds, id)
		}
		if err := scanner.Err(); err != nil {
			panic(err)
		}
		f.Close()
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

	relays, err := relay.FindOptimal(ctx, "DE", 5)
	if err != nil {
		panic(err)
	}

	go discovery.Broadcast(ctx, cert, discovery.AddressLister{Addresses: relays.ToSlice()}, discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto))

	mux := http.NewServeMux()
	mux.Handle("/", proxy)
	connChan := make(chan net.Conn, 5)
	for _, r := range relays.Relays {
		invites, err := relay.Listen(ctx, r.URL, cert)
		if err != nil {
			panic(err)
		}
		go func(invites <-chan relayprotocol.SessionInvitation) {
			for {
				select {
				case <-ctx.Done():
					return
				case inv := <-invites:
					if len(trustedIds) != 0 && !slices.ContainsFunc(trustedIds, func(deviceId protocol.DeviceID) bool {
						return slices.Equal(deviceId[:], inv.From)
					}) {
						continue
					}
					conn, _, err := relay.CreateSession(ctx, inv, cert, nil)
					if err != nil {
						log.Printf("Error on invite session: %s", err)
						continue
					}
					connChan <- conn
				}
			}
		}(invites)
	}
	log.Fatal(lib.ServeMux(ctx, mux, connChan))
}
