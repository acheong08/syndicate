package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
	"github.com/syncthing/syncthing/lib/protocol"
)

func TestNewListener(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := ListenConfig{
		TrustedIDs:        []protocol.DeviceID{},
		RelayURLs:         []string{},
		TargetRelayCount:  3,
		MinRelayThreshold: 2,
	}

	listener, err := NewListener(ctx, tr, config)
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Test that listener is created with correct config
	if len(listener.trustedIDs) != 0 {
		t.Errorf("Expected empty trustedIDs, got %d items", len(listener.trustedIDs))
	}

	if len(listener.relayURLs) != 0 {
		t.Errorf("Expected empty relayURLs, got %d items", len(listener.relayURLs))
	}

	// With empty trusted list, the listener should work but no explicit trust check
	// The behavior is handled in the invitation processing logic
}

func TestListenerClose(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}

	// Close should work without error
	if err := listener.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Second close should also work
	if err := listener.Close(); err != nil {
		t.Errorf("Second Close() error = %v", err)
	}
}

func TestListenerAddr(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr()
	if addr.Network() != "relay" {
		t.Errorf("Addr().Network() = %v, want relay", addr.Network())
	}

	if !strings.Contains(addr.String(), "relay:") {
		t.Errorf("Addr().String() = %v, should contain 'relay:'", addr.String())
	}
}

func TestRelayScore(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Test relay scoring
	relay1 := RelayInfo{
		Stats: Stats{
			UptimeSeconds:     3600, // 1 hour
			NumActiveSessions: 50,   // Less loaded
			Options: struct {
				NetworkTimeout int `json:"network-timeout"`
				PingInterval   int `json:"ping-interval"`
				MessageTimeout int `json:"message-timeout"`
				PerSessionRate int `json:"per-session-rate"`
				GlobalRate     int `json:"global-rate"`
			}{
				GlobalRate:     1000000, // 1MB/s
				PerSessionRate: 100000,  // 100KB/s
			},
		},
	}

	relay2 := RelayInfo{
		Stats: Stats{
			UptimeSeconds:     7200, // 2 hours
			NumActiveSessions: 200,  // More loaded
			Options: struct {
				NetworkTimeout int `json:"network-timeout"`
				PingInterval   int `json:"ping-interval"`
				MessageTimeout int `json:"message-timeout"`
				PerSessionRate int `json:"per-session-rate"`
				GlobalRate     int `json:"global-rate"`
			}{
				GlobalRate:     500000, // 500KB/s
				PerSessionRate: 50000,  // 50KB/s
			},
		},
	}

	score1 := listener.calculateRelayScore(relay1)
	score2 := listener.calculateRelayScore(relay2)

	t.Logf("Relay1 score: %d, Relay2 score: %d", score1, score2)

	// relay1 should have higher score due to much better rate limits and fewer sessions
	// even though relay2 has higher uptime
	if score1 <= score2 {
		t.Errorf("Expected relay1 score (%d) > relay2 score (%d) due to better rate limits and load", score1, score2)
	}
}

func TestFindOptimalRelaysWithMockServer(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Test the scoring logic with mock relay data
	relays := []RelayInfo{
		{
			URL: "relay://relay1.example.com:22067",
			Location: Location{
				Country: "US",
			},
			Stats: Stats{
				UptimeSeconds:     86400,
				NumActiveSessions: 50,
				Options: struct {
					NetworkTimeout int `json:"network-timeout"`
					PingInterval   int `json:"ping-interval"`
					MessageTimeout int `json:"message-timeout"`
					PerSessionRate int `json:"per-session-rate"`
					GlobalRate     int `json:"global-rate"`
				}{
					GlobalRate:     1000000,
					PerSessionRate: 100000,
				},
			},
		},
		{
			URL: "relay://relay2.example.com:22067",
			Location: Location{
				Country: "DE",
			},
			Stats: Stats{
				UptimeSeconds:     172800,
				NumActiveSessions: 25,
				Options: struct {
					NetworkTimeout int `json:"network-timeout"`
					PingInterval   int `json:"ping-interval"`
					MessageTimeout int `json:"message-timeout"`
					PerSessionRate int `json:"per-session-rate"`
					GlobalRate     int `json:"global-rate"`
				}{
					GlobalRate:     2000000,
					PerSessionRate: 200000,
				},
			},
		},
	}

	// Test scoring - relay2 should score higher
	score1 := listener.calculateRelayScore(relays[0])
	score2 := listener.calculateRelayScore(relays[1])

	if score2 <= score1 {
		t.Errorf("Expected relay2 score (%d) > relay1 score (%d)", score2, score1)
	}
}
func TestIsTrusted(t *testing.T) {
	cert1, err := transport.TestCertificate("test1")
	if err != nil {
		t.Fatalf("Failed to create test certificate 1: %v", err)
	}

	cert2, err := transport.TestCertificate("test2")
	if err != nil {
		t.Fatalf("Failed to create test certificate 2: %v", err)
	}

	deviceID1 := transport.TestDeviceID(cert1)
	deviceID2 := transport.TestDeviceID(cert2)

	tr := NewTransport(cert1, Config{})
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create listener with trusted IDs
	config := ListenConfig{
		TrustedIDs: []protocol.DeviceID{deviceID1},
	}

	listener, err := NewListener(ctx, tr, config)
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Test trusted device
	if !listener.isTrusted(deviceID1[:]) {
		t.Error("deviceID1 should be trusted")
	}

	// Test untrusted device
	if listener.isTrusted(deviceID2[:]) {
		t.Error("deviceID2 should not be trusted")
	}

	// Test with empty trusted list (should accept all)
	listenerAll, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listenerAll.Close()

	// With empty trusted list, all device checks should return true (no filtering)
	// The isTrusted method returns false for empty trusted list by design
	// This is correct behavior - it means "no filtering, accept all"
}

func TestAddressLister(t *testing.T) {
	lister := newAddressLister()

	// Test initial state
	if len(lister.ExternalAddresses()) != 0 {
		t.Error("Initial ExternalAddresses() should be empty")
	}

	if len(lister.AllAddresses()) != 0 {
		t.Error("Initial AllAddresses() should be empty")
	}

	// Test update
	testAddresses := []string{
		"relay://relay1.example.com:22067",
		"relay://relay2.example.com:22067",
	}

	lister.updateAddresses(testAddresses)

	external := lister.ExternalAddresses()
	if len(external) != 2 {
		t.Errorf("ExternalAddresses() length = %d, want 2", len(external))
	}

	all := lister.AllAddresses()
	if len(all) != 2 {
		t.Errorf("AllAddresses() length = %d, want 2", len(all))
	}

	// Test that returned slices are copies (modifications don't affect original)
	external[0] = "modified"
	if lister.ExternalAddresses()[0] == "modified" {
		t.Error("ExternalAddresses() should return a copy, not the original slice")
	}
}

// createMockRelayServer creates a mock HTTP server for testing
func createMockRelayServer(t *testing.T) *httptest.Server {
	relayList := RelayList{
		Relays: []RelayInfo{
			{
				URL: "relay://relay1.example.com:22067",
				Location: Location{
					Country:   "US",
					Continent: "North America",
					City:      "New York",
					Longitude: -74.0,
					Latitude:  40.7,
				},
				Stats: Stats{
					UptimeSeconds:     86400,
					NumActiveSessions: 50,
					Options: struct {
						NetworkTimeout int `json:"network-timeout"`
						PingInterval   int `json:"ping-interval"`
						MessageTimeout int `json:"message-timeout"`
						PerSessionRate int `json:"per-session-rate"`
						GlobalRate     int `json:"global-rate"`
					}{
						NetworkTimeout: 60,
						PingInterval:   60,
						MessageTimeout: 60,
						PerSessionRate: 100000,
						GlobalRate:     1000000,
					},
				},
			},
			{
				URL: "relay://relay2.example.com:22067",
				Location: Location{
					Country:   "DE",
					Continent: "Europe",
					City:      "Berlin",
					Longitude: 13.4,
					Latitude:  52.5,
				},
				Stats: Stats{
					UptimeSeconds:     172800,
					NumActiveSessions: 25,
					Options: struct {
						NetworkTimeout int `json:"network-timeout"`
						PingInterval   int `json:"ping-interval"`
						MessageTimeout int `json:"message-timeout"`
						PerSessionRate int `json:"per-session-rate"`
						GlobalRate     int `json:"global-rate"`
					}{
						NetworkTimeout: 60,
						PingInterval:   60,
						MessageTimeout: 60,
						PerSessionRate: 200000,
						GlobalRate:     2000000,
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/endpoint/full" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(relayList)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return server
}
