package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const keyringService = "big-brother-agent"
const defaultProvisionTimeout = 10 * time.Second

// Provision performs one-time provisioning: key generation and registration
func Provision(cfg *Config) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	if err := storePrivateKey(cfg.DeviceID, priv); err != nil {
		return err
	}
	return registerPublicKey(cfg, pub)
}

// registerPublicKey sends device identity + pubkey to relay provisioning endpoint.
func registerPublicKey(cfg *Config, pub ed25519.PublicKey) error {
	payload := map[string]string{
		"device_id":   cfg.DeviceID,
		"public_key":  base64.StdEncoding.EncodeToString(pub),
		"setup_token": cfg.SetupToken,
	}
	b, _ := json.Marshal(payload)
	provisionURL, err := provisionEndpoint(cfg)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, provisionURL, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("create provision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: defaultProvisionTimeout}
	if allowInsecureProvisioning(cfg) {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := client.Do(req)
	if err != nil && allowInsecureProvisioning(cfg) && strings.HasPrefix(provisionURL, "https://") && strings.Contains(err.Error(), "server gave HTTP response to HTTPS client") {
		fallbackURL := "http://" + strings.TrimPrefix(provisionURL, "https://")
		fallbackReq, reqErr := http.NewRequest(http.MethodPost, fallbackURL, bytes.NewReader(b))
		if reqErr != nil {
			return fmt.Errorf("create provision fallback request: %w", reqErr)
		}
		fallbackReq.Header.Set("Content-Type", "application/json")
		fmt.Fprintf(os.Stderr, "warning: relay_api_address appears to be HTTP; retrying provisioning over %s\n", fallbackURL)
		resp, err = client.Do(fallbackReq)
	}
	if err != nil {
		return fmt.Errorf("post provision: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("provision failed: %s", string(body))
	}

	return nil
}

// syncProvisionedDevice attempts idempotent re-registration when setup token exists.
// This helps after relay state loss without blocking runtime startup.
func syncProvisionedDevice(cfg *Config, pub ed25519.PublicKey) error {
	if strings.TrimSpace(cfg.SetupToken) == "" {
		return nil
	}
	if err := registerPublicKey(cfg, pub); err != nil {
		if strings.Contains(err.Error(), "invalid setup token") {
			return nil
		}
		return err
	}
	return nil
}

// provisionEndpoint resolves provisioning URL from config or relay host fallback.
func provisionEndpoint(cfg *Config) (string, error) {
	if strings.TrimSpace(cfg.RelayAPIAddress) != "" {
		raw := strings.TrimRight(strings.TrimSpace(cfg.RelayAPIAddress), "/")
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse relay_api_address: %w", err)
		}
		if u.Scheme != "https" && !allowInsecureProvisioning(cfg) {
			return "", fmt.Errorf("insecure provisioning endpoint scheme %q blocked; use https or set BIGBROTHER_ALLOW_INSECURE_PROVISIONING=1 for development", u.Scheme)
		}
		return raw + "/api/provision", nil
	}
	u, err := url.Parse(cfg.RelayAddress)
	if err != nil {
		return "", fmt.Errorf("parse relay address: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("parse relay address: missing host")
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   host + ":8080",
		Path:   "/api/provision",
	}
	if allowInsecureProvisioning(cfg) {
		apiURL.Scheme = "http"
	}
	return apiURL.String(), nil
}

// allowInsecureProvisioning is a development-only override for plaintext/insecure TLS provisioning.
func allowInsecureProvisioning(cfg *Config) bool {
	if cfg != nil && cfg.AllowInsecureRelay {
		return true
	}
	return strings.TrimSpace(os.Getenv("BIGBROTHER_ALLOW_INSECURE_PROVISIONING")) == "1"
}

// storePrivateKey writes key material to OS keyring, with optional file fallback.
func storePrivateKey(deviceID string, priv ed25519.PrivateKey) error {
	keyData := base64.StdEncoding.EncodeToString(priv)
	if os.Getenv("BIGBROTHER_DISABLE_KEYRING") != "1" {
		if err := keyring.Set(keyringService, deviceID, keyData); err == nil {
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "warning: keyring storage failed, falling back to file storage: %v\n", err)
		}
	}
	if os.Getenv("BIGBROTHER_ALLOW_KEYFILE_FALLBACK") == "1" {
		return writeKeyFile(deviceID, priv)
	}
	return fmt.Errorf("secure key storage unavailable and fallback not enabled")
}

// writeKeyFile writes key bytes to a restricted local file when fallback is enabled.
func writeKeyFile(deviceID string, priv ed25519.PrivateKey) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("user config dir: %w", err)
	}
	appdir := filepath.Join(dir, "big-brother")
	if err := os.MkdirAll(filepath.Join(appdir, "keys"), 0700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	keyPath := filepath.Join(appdir, "keys", deviceID+".key")
	return os.WriteFile(keyPath, priv, 0600)
}

// loadPrivateKey reads key from keyring first, then optional fallback file.
func loadPrivateKey(deviceID string) (ed25519.PrivateKey, error) {
	if os.Getenv("BIGBROTHER_DISABLE_KEYRING") != "1" {
		secret, err := keyring.Get(keyringService, deviceID)
		if err == nil {
			decoded, err := base64.StdEncoding.DecodeString(secret)
			if err != nil {
				return nil, fmt.Errorf("decode key: %w", err)
			}
			return ed25519.PrivateKey(decoded), nil
		}
		fmt.Fprintf(os.Stderr, "warning: keyring read failed, falling back to file: %v\n", err)
	}
	if os.Getenv("BIGBROTHER_ALLOW_KEYFILE_FALLBACK") == "1" {
		return readKeyFile(deviceID)
	}
	return nil, fmt.Errorf("secure key retrieval unavailable and fallback not enabled")
}

// readKeyFile loads fallback key bytes from user config directory.
func readKeyFile(deviceID string) (ed25519.PrivateKey, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("user config dir: %w", err)
	}
	keyPath := filepath.Join(dir, "big-brother", "keys", deviceID+".key")
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return ed25519.PrivateKey(raw), nil
}
