package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// provisionReq is the one-time registration payload posted by a new agent.
type provisionReq struct {
	DeviceID   string `json:"device_id"`
	PublicKey  string `json:"public_key"`
	SetupToken string `json:"setup_token"`
}

// authPayload is the signed identity handshake sent over the TLS agent socket.
type authPayload struct {
	DeviceID  string `json:"device_id"`
	PubKey    string `json:"pubkey_b64"`
	Sig       string `json:"sig_b64"`
	Timestamp string `json:"timestamp"`
}

var (
	mu sync.Mutex
	// deviceID -> base64 pubkey
	devices = map[string]string{}
	// one-time setup tokens
	setupTokens = map[string]string{}
	// active agent sessions
	sessions        = map[string]*agentSession{}
	deviceStorePath = defaultDeviceStorePath
	controllerToken string
)

const defaultDeviceStorePath = "relay-devices.json"

// agentSession tracks a connected agent and response channels for in-flight requests.
type agentSession struct {
	deviceID  string
	conn      net.Conn
	enc       *json.Encoder
	dec       *json.Decoder
	respMu    sync.Mutex
	responses map[string]chan map[string]interface{}
}

// newAgentSession wires JSON encoder/decoder around accepted connection.
func newAgentSession(deviceID string, c net.Conn) *agentSession {
	return &agentSession{
		deviceID:  deviceID,
		conn:      c,
		enc:       json.NewEncoder(c),
		dec:       json.NewDecoder(c),
		responses: make(map[string]chan map[string]interface{}),
	}
}

// sendRequest sends command to agent and waits for matching response id.
func (s *agentSession) sendRequest(req map[string]string) (map[string]interface{}, error) {
	id := req["id"]
	if id == "" {
		return nil, fmt.Errorf("missing request id")
	}
	ch := make(chan map[string]interface{}, 1)

	s.respMu.Lock()
	s.responses[id] = ch
	s.respMu.Unlock()

	defer func() {
		s.respMu.Lock()
		delete(s.responses, id)
		s.respMu.Unlock()
	}()

	if err := s.enc.Encode(req); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response")
	}
}

// readLoop dispatches incoming agent messages to waiting response channels.
func (s *agentSession) readLoop() {
	for {
		var msg map[string]interface{}
		if err := s.dec.Decode(&msg); err != nil {
			log.Printf("agent %s disconnected: %v", s.deviceID, err)
			break
		}
		if msg["type"] == "resp" {
			id, ok := msg["id"].(string)
			if !ok || id == "" {
				log.Printf("agent %s sent response without id: %v", s.deviceID, msg)
				continue
			}
			s.respMu.Lock()
			ch, ok := s.responses[id]
			s.respMu.Unlock()
			if ok {
				select {
				case ch <- msg:
				default:
				}
			} else {
				log.Printf("agent %s response channel missing for id %s", s.deviceID, id)
			}
		} else {
			log.Printf("agent %s sent message: %v", s.deviceID, msg)
		}
	}
}

// main initializes persistent state, starts HTTP API, and starts TLS agent listener.
func main() {
	deviceStorePath = getenv("RELAY_DEVICE_STORE_FILE")
	if deviceStorePath == "" {
		deviceStorePath = defaultDeviceStorePath
	}
	if err := loadDevicesFromFile(deviceStorePath); err != nil {
		log.Fatalf("load devices: %v", err)
	}

	// Load one-time setup tokens from environment or file.
	if v := getenv("RELAY_SETUP_TOKEN_FILE"); v != "" {
		if err := loadSetupTokensFromFile(v); err != nil {
			log.Fatalf("load setup tokens: %v", err)
		}
	}
	loadSetupTokensFromEnv()
	if len(setupTokens) == 0 {
		log.Println("warning: no setup tokens configured; device provisioning disabled")
	}

	controllerToken = strings.TrimSpace(getenv("RELAY_CONTROLLER_TOKEN"))
	if controllerToken == "" {
		log.Fatalf("missing RELAY_CONTROLLER_TOKEN: controller authentication is required for relay control APIs")
	}

	httpPort := getenv("RELAY_HTTP_PORT")
	if httpPort == "" {
		httpPort = "8080"
	}
	// HTTP API serves provisioning and controller endpoints.
	http.HandleFunc("/api/provision", handleProvision)
	http.HandleFunc("/api/exec", handleExec)
	http.HandleFunc("/api/devices", handleDevices)
	http.HandleFunc("/api/lock", func(w http.ResponseWriter, r *http.Request) { handleSimpleAction(w, r, "lock") })
	http.HandleFunc("/api/locate", func(w http.ResponseWriter, r *http.Request) { handleSimpleAction(w, r, "locate") })

	go func() {
		log.Printf("HTTP API listening on :%s", httpPort)
		log.Fatal(http.ListenAndServe(":"+httpPort, nil))
	}()

	// Start TLS listener for agent connections.
	// Prefer explicit certs for production; self-signed fallback is for local testing.
	serverCertPath := "" // OPTIONAL: set RELAY_SERVER_CERT
	serverKeyPath := ""  // OPTIONAL: set RELAY_SERVER_KEY
	caPath := ""         // OPTIONAL: set RELAY_CLIENT_CA (PEM) to verify client certs
	if v := getenv("RELAY_SERVER_CERT"); v != "" {
		serverCertPath = v
	}
	if v := getenv("RELAY_SERVER_KEY"); v != "" {
		serverKeyPath = v
	}
	if v := getenv("RELAY_CLIENT_CA"); v != "" {
		caPath = v
	}
	requireMTLS := strings.TrimSpace(getenv("RELAY_REQUIRE_MTLS")) == "1"

	var cfg *tls.Config
	if serverCertPath != "" && serverKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
		if err != nil {
			log.Fatalf("load server cert: %v", err)
		}
		cfg = &tls.Config{Certificates: []tls.Certificate{cert}}
		if caPath != "" {
			caPEM, err := os.ReadFile(caPath)
			if err != nil {
				log.Fatalf("read ca: %v", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				log.Fatalf("failed to append CA certs")
			}
			cfg.ClientCAs = pool
			if requireMTLS {
				cfg.ClientAuth = tls.RequireAndVerifyClientCert
				log.Printf("agent mTLS is enabled (RELAY_REQUIRE_MTLS=1)")
			} else {
				log.Printf("agent mTLS CA loaded but not required (set RELAY_REQUIRE_MTLS=1 to enforce client certs)")
			}
		} else if requireMTLS {
			log.Fatalf("RELAY_REQUIRE_MTLS=1 requires RELAY_CLIENT_CA to be set")
		}
	} else {
		certPEM, keyPEM, err := generateSelfSigned()
		if err != nil {
			log.Fatalf("generate cert: %v", err)
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			log.Fatalf("x509 keypair: %v", err)
		}
		cfg = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	tlsPort := getenv("RELAY_TLS_PORT")
	if tlsPort == "" {
		tlsPort = "8443"
	}
	ln, err := tls.Listen("tcp", ":"+tlsPort, cfg)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("TLS agent listener on :%s", tlsPort)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleAgentConn(c)
	}
}

// handleProvision validates setup token, stores device key, and persists device map.
func handleProvision(w http.ResponseWriter, r *http.Request) {
	var p provisionReq
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mu.Lock()
	expected, ok := setupTokens[p.DeviceID]
	if !ok || expected != p.SetupToken {
		mu.Unlock()
		log.Printf("invalid setup token for %s", p.DeviceID)
		http.Error(w, "invalid setup token", http.StatusUnauthorized)
		return
	}
	delete(setupTokens, p.DeviceID)
	devices[p.DeviceID] = p.PublicKey
	if err := saveDevicesToFile(deviceStorePath); err != nil {
		mu.Unlock()
		log.Printf("persist devices failed: %v", err)
		http.Error(w, "persist devices failed", http.StatusInternalServerError)
		return
	}
	mu.Unlock()
	log.Printf("provisioned %s", p.DeviceID)
	w.WriteHeader(http.StatusOK)
}

// execReq is shared request shape used by exec/lock/locate controller endpoints.
type execReq struct {
	DeviceID string `json:"device_id"`
	Cmd      string `json:"cmd"`
}

// handleExec forwards command execution request to an active agent session.
func handleExec(w http.ResponseWriter, r *http.Request) {
	if !authorizeControllerRequest(w, r) {
		return
	}
	var e execReq
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mu.Lock()
	s, ok := sessions[e.DeviceID]
	mu.Unlock()
	if !ok {
		http.Error(w, "device offline", http.StatusServiceUnavailable)
		return
	}
	resp, err := s.sendRequest(map[string]string{"type": "exec", "id": "1", "cmd": e.Cmd})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

// handleDevices lists all known provisioned device IDs.
func handleDevices(w http.ResponseWriter, r *http.Request) {
	if !authorizeControllerRequest(w, r) {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	ids := make([]string, 0, len(devices))
	for id := range devices {
		ids = append(ids, id)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"devices": ids})
}

// handleSimpleAction forwards non-shell actions (lock/locate) to active session.
func handleSimpleAction(w http.ResponseWriter, r *http.Request, action string) {
	if !authorizeControllerRequest(w, r) {
		return
	}
	var e execReq
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mu.Lock()
	s, ok := sessions[e.DeviceID]
	mu.Unlock()
	if !ok {
		http.Error(w, "device offline", http.StatusServiceUnavailable)
		return
	}
	req := map[string]string{"type": action, "id": "1", "cmd": e.Cmd}
	resp, err := s.sendRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

// handleAgentConn verifies signed auth payload and registers live agent session.
func handleAgentConn(c net.Conn) {
	defer c.Close()
	dec := json.NewDecoder(c)
	var m map[string]interface{}
	if err := dec.Decode(&m); err != nil {
		log.Printf("read auth: %v", err)
		return
	}
	if m["type"] != "auth" {
		log.Printf("expected auth, got %v", m["type"])
		return
	}
	authMap, ok := m["auth"].(map[string]interface{})
	if !ok {
		log.Printf("invalid auth payload")
		return
	}
	var auth authPayload
	authBytes, err := json.Marshal(authMap)
	if err != nil {
		log.Printf("invalid auth payload: %v", err)
		return
	}
	if err := json.Unmarshal(authBytes, &auth); err != nil {
		log.Printf("invalid auth payload: %v", err)
		return
	}

	if auth.DeviceID == "" || auth.PubKey == "" || auth.Sig == "" || auth.Timestamp == "" {
		log.Printf("incomplete auth payload: %+v", auth)
		return
	}

	pubBytes, err := base64.StdEncoding.DecodeString(auth.PubKey)
	if err != nil {
		log.Printf("invalid public key base64: %v", err)
		return
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		log.Printf("invalid public key size: %d", len(pubBytes))
		return
	}
	pub := ed25519.PublicKey(pubBytes)

	sigBytes, err := base64.StdEncoding.DecodeString(auth.Sig)
	if err != nil {
		log.Printf("invalid signature base64: %v", err)
		return
	}
	if len(sigBytes) != ed25519.SignatureSize {
		log.Printf("invalid signature size: %d", len(sigBytes))
		return
	}

	msg := []byte(auth.DeviceID + "|" + auth.Timestamp)
	if !ed25519.Verify(pub, msg, sigBytes) {
		log.Printf("signature verification failed for %s", auth.DeviceID)
		return
	}

	ts, err := time.Parse(time.RFC3339, auth.Timestamp)
	if err != nil {
		log.Printf("invalid timestamp format: %v", err)
		return
	}
	if time.Since(ts) > 2*time.Minute || time.Until(ts) > 2*time.Minute {
		log.Printf("auth timestamp out of range: %s", auth.Timestamp)
		return
	}

	mu.Lock()
	expected, ok := devices[auth.DeviceID]
	if !ok || expected != auth.PubKey {
		mu.Unlock()
		log.Printf("unauthorized device %s", auth.DeviceID)
		return
	}
	s := newAgentSession(auth.DeviceID, c)
	sessions[auth.DeviceID] = s
	mu.Unlock()

	log.Printf("agent %s connected", auth.DeviceID)
	s.readLoop()

	mu.Lock()
	delete(sessions, auth.DeviceID)
	mu.Unlock()
}

// generateSelfSigned creates localhost-only cert/key for development convenience.
func generateSelfSigned() ([]byte, []byte, error) {
	// minimal self-signed cert for localhost
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, pub, priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM, nil
}

// loadSetupTokensFromEnv parses RELAY_SETUP_TOKENS as comma-separated device=token pairs.
func loadSetupTokensFromEnv() {
	pairs := strings.Split(getenv("RELAY_SETUP_TOKENS"), ",")
	for _, pair := range pairs {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		deviceID := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])
		if deviceID != "" && token != "" {
			setupTokens[deviceID] = token
		}
	}
}

// loadDevicesFromFile restores persisted device public key map.
func loadDevicesFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stored := map[string]string{}
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	mu.Lock()
	devices = stored
	mu.Unlock()
	return nil
}

// saveDevicesToFile persists current device key map for relay restart durability.
func saveDevicesToFile(path string) error {
	stored := make(map[string]string, len(devices))
	for deviceID, publicKey := range devices {
		stored[deviceID] = publicKey
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o600)
}

// loadSetupTokensFromFile reads JSON object of device tokens.
func loadSetupTokensFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return err
	}
	for id, token := range tokens {
		setupTokens[id] = token
	}
	return nil
}

// getenv returns environment value or empty string if unset.
func getenv(k string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return ""
}

// authorizeControllerRequest enforces token-based auth on relay control endpoints.
func authorizeControllerRequest(w http.ResponseWriter, r *http.Request) bool {
	if controllerToken == "" {
		http.Error(w, "controller authentication not configured", http.StatusServiceUnavailable)
		return false
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return false
	}
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		http.Error(w, "invalid authorization format", http.StatusUnauthorized)
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if provided == "" {
		http.Error(w, "missing authorization token", http.StatusUnauthorized)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(controllerToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}
