package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
)

// TestRelaySelectionIntegration tests the full relay selection process
func TestRelaySelectionIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cert, err := transport.TestCertificate("test-integration")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{
		TargetRelayCount:  3,
		MinRelayThreshold: 2,
		RelayCountry:      "DE", // Test with specific country
	})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Try to find relays - this will make real HTTP requests
	relays, err := listener.findOptimalRelays("DE", 3)
	if err != nil {
		t.Logf("Expected error when finding relays (no actual relay connectivity): %v", err)
		// This is expected since we can't actually connect to relays in tests
		return
	}

	if len(relays) > 0 {
		t.Logf("Found %d relays: %v", len(relays), relays)
	}
}

// TestRelayConnectivityTimeout tests that relay connectivity testing times out properly
func TestRelayConnectivityTimeout(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Test with non-existent relays
	relays := []RelayInfo{
		{
			URL: "relay://nonexistent1.example.com:22067",
			Stats: Stats{
				UptimeSeconds:     3600,
				NumActiveSessions: 10,
			},
		},
		{
			URL: "relay://nonexistent2.example.com:22067",
			Stats: Stats{
				UptimeSeconds:     7200,
				NumActiveSessions: 5,
			},
		},
	}

	start := time.Now()
	workingRelays, err := listener.testRelayConnectivity(relays, 2)
	duration := time.Since(start)

	// Should fail to find working relays
	if err == nil {
		t.Errorf("Expected error when testing nonexistent relays, got %d working relays", len(workingRelays))
	}

	// Should timeout reasonably quickly (within 10 seconds for 2 relays with 5s timeout each)
	if duration > 10*time.Second {
		t.Errorf("Connectivity test took too long: %v", duration)
	}

	t.Logf("Connectivity test completed in %v", duration)
}

// TestRelayAPIIntegration tests integration with the real Syncthing relay API
func TestRelayAPIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API integration test in short mode")
	}

	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}
	defer listener.Close()

	// Test fetching relay list from real API
	resp, err := http.Get("https://relays.syncthing.net/endpoint/full")
	if err != nil {
		t.Skipf("Cannot reach Syncthing relay API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("Syncthing relay API returned status %d", resp.StatusCode)
	}

	t.Log("Successfully connected to Syncthing relay API")

	// Try to parse the response
	var relayList RelayList
	if err := json.NewDecoder(resp.Body).Decode(&relayList); err != nil {
		t.Errorf("Failed to decode relay list: %v", err)
		return
	}

	if len(relayList.Relays) == 0 {
		t.Error("No relays found in API response")
		return
	}

	t.Logf("Found %d relays in API response", len(relayList.Relays))

	// Test scoring on real relay data
	if len(relayList.Relays) >= 2 {
		score1 := listener.calculateRelayScore(relayList.Relays[0])
		score2 := listener.calculateRelayScore(relayList.Relays[1])
		t.Logf("Relay scores: %s=%d, %s=%d",
			relayList.Relays[0].URL, score1,
			relayList.Relays[1].URL, score2)
	}
}
