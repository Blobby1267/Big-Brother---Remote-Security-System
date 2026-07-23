package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultCommandTimeout = 30 * time.Second

// wireMsg is the JSON envelope used between relay and agent.
type wireMsg struct {
	Type string       `json:"type"`
	ID   string       `json:"id,omitempty"`
	Cmd  string       `json:"cmd,omitempty"`
	Auth *authPayload `json:"auth,omitempty"`
	// response fields
	ExitCode int    `json:"exit_code,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

// authPayload contains signed identity proof sent once per new connection.
type authPayload struct {
	DeviceID  string `json:"device_id"`
	PubKey    string `json:"pubkey_b64"`
	Sig       string `json:"sig_b64"`
	Timestamp string `json:"timestamp"`
}

// runAgentRuntime maintains a persistent outbound TLS session to relay.
func runAgentRuntime(cfg *Config) error {
	priv, err := loadPrivateKey(cfg.DeviceID)
	if err != nil {
		return fmt.Errorf("load private key: %w", err)
	}
	pub := priv.Public().(ed25519.PublicKey)

	u, err := url.Parse(cfg.RelayAddress)
	if err != nil {
		return fmt.Errorf("parse relay address: %w", err)
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		// default port
		host = net.JoinHostPort(host, "443")
	}

	// TLS config can be tuned with env vars for CA pinning and client certificates.
	var tlsCfg tls.Config
	if ca := os.Getenv("BIGBROTHER_CA"); ca != "" {
		caPEM, err := os.ReadFile(ca)
		if err != nil {
			return fmt.Errorf("read ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return fmt.Errorf("append CA certs failed")
		}
		tlsCfg.RootCAs = pool
	} else {
		roots, _ := x509.SystemCertPool()
		tlsCfg.RootCAs = roots
	}
	if cfg.AllowInsecureRelay {
		tlsCfg.InsecureSkipVerify = true
		log.Printf("warning: relay TLS certificate verification disabled by config (allow_insecure_relay=true)")
	} else if os.Getenv("BIGBROTHER_INSECURE") == "1" && os.Getenv("BIGBROTHER_ENABLE_INSECURE_TLS") == "1" {
		tlsCfg.InsecureSkipVerify = true
		log.Printf("warning: relay TLS certificate verification disabled (development-only mode)")
	} else if os.Getenv("BIGBROTHER_INSECURE") == "1" {
		log.Printf("warning: insecure relay requested but ignored; set BIGBROTHER_ENABLE_INSECURE_TLS=1 only for local development")
	}

	// Optional client certificate enables full mutual TLS when configured.
	clientCert := os.Getenv("BIGBROTHER_CLIENT_CERT")
	clientKey := os.Getenv("BIGBROTHER_CLIENT_KEY")
	if clientCert != "" && clientKey != "" {
		cert, err := tls.LoadX509KeyPair(clientCert, clientKey)
		if err != nil {
			return fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Reconnect loop with bounded exponential backoff.
	tlsCfgPtr := &tlsCfg
	backoff := time.Second
	for {
		// Non-fatal re-sync improves recovery after relay restarts/state resets.
		if err := syncProvisionedDevice(cfg, pub); err != nil {
			log.Printf("registration sync failed (continuing): %v", err)
		}
		log.Printf("connecting to relay %s", host)
		conn, err := tls.Dial("tcp", host, tlsCfgPtr)
		if err != nil {
			log.Printf("dial error: %v; retrying in %v", err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}
		backoff = time.Second

		if err := handleConnection(conn, cfg.DeviceID, priv, pub); err != nil {
			log.Printf("connection handling error: %v", err)
		}
		_ = conn.Close()
		time.Sleep(2 * time.Second)
	}
}

// handleConnection authenticates once, then processes relay requests until disconnect.
func handleConnection(rw io.ReadWriter, deviceID string, priv ed25519.PrivateKey, pub ed25519.PublicKey) error {
	enc := json.NewEncoder(rw)
	dec := json.NewDecoder(bufio.NewReader(rw))

	// Send signed timestamp-based auth to prevent replay attacks.
	ts := time.Now().UTC().Format(time.RFC3339)
	toSign := []byte(deviceID + "|" + ts)
	sig := ed25519.Sign(priv, toSign)
	auth := &authPayload{
		DeviceID:  deviceID,
		PubKey:    base64.StdEncoding.EncodeToString(pub),
		Sig:       base64.StdEncoding.EncodeToString(sig),
		Timestamp: ts,
	}
	if err := enc.Encode(&wireMsg{Type: "auth", Auth: auth}); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// Read/dispatch loop for incoming relay messages.
	for {
		var m wireMsg
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return fmt.Errorf("read message: %w", err)
		}

		switch m.Type {
		case "exec":
			out, code, err := runCommand(m.Cmd)
			resp := wireMsg{Type: "resp", ID: m.ID, ExitCode: code, Output: out}
			if err != nil {
				resp.Error = err.Error()
			}
			if err := enc.Encode(&resp); err != nil {
				return fmt.Errorf("send resp: %w", err)
			}
		case "ping":
			_ = enc.Encode(&wireMsg{Type: "pong"})
		default:
			log.Printf("unknown message type: %s", m.Type)
		}
	}
}

// runCommand executes shell command with timeout and returns output + exit status.
func runCommand(cmd string) (string, int, error) {
	if cmd == "" {
		return "", 1, fmt.Errorf("empty command")
	}
	// Optional timeout override in seconds via BIGBROTHER_COMMAND_TIMEOUT.
	timeout := defaultCommandTimeout
	if raw := strings.TrimSpace(os.Getenv("BIGBROTHER_COMMAND_TIMEOUT")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var c *exec.Cmd
	// Command execution shell differs by platform.
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", cmd)
	}
	b, err := c.CombinedOutput()
	code := 0
	// Map timeout to conventional status code 124 for easier operator handling.
	if ctx.Err() == context.DeadlineExceeded {
		return string(b), 124, fmt.Errorf("command timed out after %s", timeout)
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return string(b), code, err
}
