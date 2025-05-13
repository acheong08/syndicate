package main

import (
	"context"
	"log"
	"net"
	"net/http"

	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/syncthing/syncthing/lib/logger"
	"github.com/syncthing/syncthing/lib/protocol"
)

func main() {
	logger.DefaultLogger.AddHandler(logger.LevelVerbose, func(l logger.LogLevel, msg string) {
		log.Println(l, msg)
	})
	cert, _ := crypto.NewCertificate("syncthing", 1)
	log.Printf("Server ID: %s", protocol.NewDeviceID(cert.Certificate[0]))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/eggs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "magic" {
			w.WriteHeader(400)
		}
		w.Write([]byte("Hello world"))
	})
	connChan := make(chan net.Conn, 5)
	go lib.StartRelayManager(ctx, cert, []protocol.DeviceID{}, connChan, "")
	log.Fatal(lib.ServeMux(ctx, mux, connChan))
}
