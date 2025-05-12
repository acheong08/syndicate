package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"net/http/httputil"

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
	flag.Parse()
	if *target == "" {
		log.Fatal("The --target flag is required")
	}
	targetURL, err := url.Parse(*target)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = &http.Transport{Proxy: nil}
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host
		req.URL.Path, req.URL.RawPath = joinURLPath(targetURL, req.URL)
		log.Println(req.URL)
	}

	cert, _ := crypto.NewCertificate("syncthing", 1)
	log.Printf("Server ID: %s", protocol.NewDeviceID(cert.Certificate[0]))
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
					conn, sni, err := relay.CreateSession(ctx, inv, cert, nil)
					if err != nil {
						log.Printf("Error on invite session: %s", err)
						continue
					}
					log.Printf("Received connection from SNI %s", sni)
					connChan <- conn
				}
			}
		}(invites)
	}
	log.Fatal(lib.ServeMux(ctx, mux, connChan))
}
