package main

import (
	"encoding/json"
	"os"
)

// Config defines all runtime/provisioning settings loaded from USB or local config.json.
type Config struct {
	// DeviceID is the stable identity used by relay to authorize this agent.
	DeviceID string `json:"device_id"`
	// SetupToken is a one-time token consumed during provisioning/key registration.
	SetupToken string `json:"setup_token"`
	// RelayAddress is the TLS endpoint used for long-lived agent runtime connections.
	RelayAddress string `json:"relay_address"`
	// RelayAPIAddress is the HTTP provisioning endpoint override (optional).
	RelayAPIAddress string `json:"relay_api_address,omitempty"`
	// AllowInsecureRelay disables TLS verification for local/self-signed testing.
	AllowInsecureRelay bool `json:"allow_insecure_relay,omitempty"`
}

// LoadConfig parses JSON config from disk and returns a typed configuration object.
func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c Config
	dec := json.NewDecoder(f)
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
