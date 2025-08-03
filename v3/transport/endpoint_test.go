package transport

import (
	"testing"

	"github.com/syncthing/syncthing/lib/protocol"
)

func TestRelayEndpoint(t *testing.T) {
	// Create a test certificate to get a valid device ID
	cert, err := TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}
	deviceID := TestDeviceID(cert)
	deviceIDStr := deviceID.String()

	tests := []struct {
		name      string
		address   string
		wantErr   bool
		wantSNI   string
		wantValid bool
	}{
		{
			name:      "simple device address",
			address:   deviceIDStr + ".syncthing",
			wantErr:   false,
			wantSNI:   "",
			wantValid: true,
		},
		{
			name:      "address with SNI",
			address:   "api." + deviceIDStr + ".syncthing",
			wantErr:   false,
			wantSNI:   "api",
			wantValid: true,
		},
		{
			name:      "complex SNI",
			address:   "api.v1.service." + deviceIDStr + ".syncthing",
			wantErr:   false,
			wantSNI:   "api.v1.service",
			wantValid: true,
		},
		{
			name:      "address with port",
			address:   deviceIDStr + ".syncthing:443",
			wantErr:   false,
			wantSNI:   "",
			wantValid: true,
		},
		{
			name:      "SNI with port",
			address:   "api." + deviceIDStr + ".syncthing:443",
			wantErr:   false,
			wantSNI:   "api",
			wantValid: true,
		},
		{
			name:      "invalid - no .syncthing suffix",
			address:   deviceIDStr + ".invalid",
			wantErr:   true,
			wantValid: false,
		},
		{
			name:      "invalid - too short",
			address:   "short.syncthing",
			wantErr:   true,
			wantValid: false,
		},
		{
			name:      "invalid - empty",
			address:   "",
			wantErr:   true,
			wantValid: false,
		},
		{
			name:      "invalid device ID",
			address:   "INVALID_DEVICE_ID.syncthing",
			wantErr:   true,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := NewRelayEndpoint(tt.address)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewRelayEndpoint() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("NewRelayEndpoint() unexpected error: %v", err)
				return
			}

			if endpoint.SNI() != tt.wantSNI {
				t.Errorf("SNI() = %v, want %v", endpoint.SNI(), tt.wantSNI)
			}

			if endpoint.Network() != "relay" {
				t.Errorf("Network() = %v, want relay", endpoint.Network())
			}

			// Test that Address() returns a valid address
			addr := endpoint.Address()
			if addr == "" {
				t.Errorf("Address() returned empty string")
			}

			// Test metadata
			meta := endpoint.Metadata()
			if _, ok := meta["device_id"]; !ok {
				t.Errorf("Metadata() missing device_id")
			}

			if tt.wantSNI != "" {
				if sni, ok := meta["sni"]; !ok || sni != tt.wantSNI {
					t.Errorf("Metadata() sni = %v, want %v", sni, tt.wantSNI)
				}
			}
		})
	}
}

func TestRelayEndpointWithRelays(t *testing.T) {
	// Create a test device ID
	cert, err := TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}
	deviceID := TestDeviceID(cert)

	relays := []string{
		"relay://relay1.syncthing.net:22067",
		"relay://relay2.syncthing.net:22067",
	}

	endpoint := NewRelayEndpointWithRelays(deviceID, "api", relays)

	if endpoint.DeviceID() != deviceID {
		t.Errorf("DeviceID() = %v, want %v", endpoint.DeviceID(), deviceID)
	}

	if endpoint.SNI() != "api" {
		t.Errorf("SNI() = %v, want api", endpoint.SNI())
	}

	if len(endpoint.Relays()) != 2 {
		t.Errorf("Relays() length = %d, want 2", len(endpoint.Relays()))
	}

	// Test SetRelays
	newRelays := []string{"relay://relay3.syncthing.net:22067"}
	endpoint.SetRelays(newRelays)

	if len(endpoint.Relays()) != 1 {
		t.Errorf("After SetRelays(), Relays() length = %d, want 1", len(endpoint.Relays()))
	}

	// Test metadata includes relays
	meta := endpoint.Metadata()
	if relaysMeta, ok := meta["relays"]; !ok {
		t.Errorf("Metadata() missing relays")
	} else if relaysSlice, ok := relaysMeta.([]string); !ok || len(relaysSlice) != 1 {
		t.Errorf("Metadata() relays = %v, want %v", relaysMeta, newRelays)
	}
}

func TestRelayEndpointAddressGeneration(t *testing.T) {
	cert, err := TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}
	deviceID := TestDeviceID(cert)

	tests := []struct {
		name         string
		deviceID     protocol.DeviceID
		sni          string
		expectedAddr string
	}{
		{
			name:         "no SNI",
			deviceID:     deviceID,
			sni:          "",
			expectedAddr: deviceID.String() + ".syncthing",
		},
		{
			name:         "with SNI",
			deviceID:     deviceID,
			sni:          "api",
			expectedAddr: "api." + deviceID.String() + ".syncthing",
		},
		{
			name:         "complex SNI",
			deviceID:     deviceID,
			sni:          "api.v1.service",
			expectedAddr: "api.v1.service." + deviceID.String() + ".syncthing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := NewRelayEndpointWithRelays(tt.deviceID, tt.sni, nil)
			addr := endpoint.Address()

			if addr != tt.expectedAddr {
				t.Errorf("Address() = %v, want %v", addr, tt.expectedAddr)
			}
		})
	}
}
