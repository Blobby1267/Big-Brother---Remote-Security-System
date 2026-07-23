package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
)

const controllerGUIBaseURL = "http://127.0.0.1:18080"

var relay = flag.String("relay", "http://localhost:8080", "relay base URL")
var certPath = flag.String("cert", "", "client cert (PEM) for mTLS")
var keyPath = flag.String("key", "", "client key (PEM) for mTLS")
var caPath = flag.String("ca", "", "CA cert (PEM) to verify relay server")
var token = flag.String("token", "", "controller auth token for relay (or BIGBROTHER_CONTROLLER_TOKEN)")

// main selects CLI command mode or launches the local web GUI when no command is given.
func main() {
	flag.Parse()
	if flag.NArg() >= 1 {
		cmd := flag.Arg(0)
		switch cmd {
		case "list":
			list()
			return
		case "shell", "exec":
			if flag.NArg() < 3 {
				usage()
				os.Exit(2)
			}
			shell(flag.Arg(1), joinCommandArgs(flag.Args()[2:]))
			return
		case "lock":
			if flag.NArg() < 2 {
				usage()
				os.Exit(2)
			}
			lock(flag.Arg(1))
			return
		case "locate":
			if flag.NArg() < 2 {
				usage()
				os.Exit(2)
			}
			locate(flag.Arg(1))
			return
		}
	}
	launchGUI()
}

// usage prints command-line help for non-GUI mode.
func usage() {
	fmt.Println("usage:")
	fmt.Println("  controller [--relay http://host:8080] [--token <token>] list")
	fmt.Println("  controller shell <device_id> <cmd...>")
	fmt.Println("  controller exec <device_id> <cmd...>")
	fmt.Println("  controller lock <device_id>")
	fmt.Println("  controller locate <device_id>")
	fmt.Println("  controller (no args) launches the desktop GUI")
}

// controllerToken returns CLI token override or environment fallback.
func controllerToken() string {
	if v := strings.TrimSpace(*token); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("BIGBROTHER_CONTROLLER_TOKEN"))
}

// joinCommandArgs rebuilds shell command text from trailing CLI arguments.
func joinCommandArgs(args []string) string {
	return strings.TrimSpace(strings.Join(args, " "))
}

// normalizeRelayURL ensures relay URL has scheme and no trailing slash.
func normalizeRelayURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "http://localhost:8080"
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return strings.TrimRight(value, "/")
	}
	return "http://" + strings.TrimRight(value, "/")
}

// launchGUI starts a local HTTP server and browser-based controller UI.
func launchGUI() {
	csrfToken, err := generateCSRFToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to initialize CSRF token: %v\n", err)
		return
	}

	// Embedded HTML template contains complete controller single-page UI.
	guiTemplate := template.Must(template.New("gui").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>Big Brother Controller</title>
  <style>
		:root { color-scheme: dark; }
		body { font-family: Segoe UI, Arial, sans-serif; margin: 0; background: linear-gradient(145deg, #09111f 0%, #101826 100%); color: #f8fafc; min-height: 100vh; }
		.shell { max-width: 1100px; margin: 0 auto; padding: 24px; }
		.card { background: rgba(12, 19, 31, 0.94); border: 1px solid #23314b; border-radius: 20px; padding: 24px; box-shadow: 0 18px 48px rgba(0,0,0,0.30); }
		h1 { margin: 0 0 6px; font-size: 30px; }
		p { margin: 0 0 18px; color: #9fb0c7; }
		.toolbar { display: grid; grid-template-columns: 1fr auto; gap: 12px; margin-bottom: 16px; }
		.workspace { display: grid; grid-template-columns: 280px 1fr; gap: 18px; }
		.panel { border: 1px solid #23314b; border-radius: 16px; background: rgba(5, 12, 22, 0.55); padding: 16px; }
		.panel h2 { margin: 0 0 10px; font-size: 15px; text-transform: uppercase; letter-spacing: 0.08em; color: #9fb0c7; }
		.field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
		label { font-size: 12px; font-weight: 700; letter-spacing: 0.03em; color: #c9d5e6; }
		input, textarea { width: 100%; padding: 11px 12px; border-radius: 12px; border: 1px solid #31425f; background: #08111e; color: #f8fafc; box-sizing: border-box; }
		textarea { min-height: 120px; font-family: Consolas, monospace; resize: vertical; }
		.output { min-height: 220px; }
		button { cursor: pointer; border: none; border-radius: 12px; padding: 11px 14px; font-weight: 700; color: #f8fafc; background: linear-gradient(135deg, #1d4ed8 0%, #2563eb 100%); }
		button.secondary { background: #1f2c43; }
		button.ghost { background: transparent; border: 1px solid #31425f; }
		button:disabled { opacity: 0.55; cursor: not-allowed; }
		.status { margin-bottom: 16px; padding: 12px 14px; border-radius: 12px; background: #142033; color: #f8fafc; }
		.status.error { background: #6f1d1b; }
		.status.ok { background: #1d4d39; }
		.agent-list { display: flex; flex-direction: column; gap: 8px; max-height: 420px; overflow-y: auto; }
		.agent-item { text-align: left; background: #0e1727; border: 1px solid #253756; }
		.agent-item.active { background: linear-gradient(135deg, #0f3b66 0%, #125796 100%); border-color: #4f8cd6; }
		.muted { color: #8ea2be; font-size: 12px; }
		.quick { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 14px; }
		.quick button { background: #162235; padding: 8px 10px; font-size: 13px; }
		.section-label { margin: 12px 0 8px; font-size: 11px; font-weight: 700; letter-spacing: 0.08em; color: #8ea2be; text-transform: uppercase; }
		.row { display: flex; gap: 10px; align-items: center; }
		@media (max-width: 860px) {
			.toolbar, .workspace { grid-template-columns: 1fr; }
		}
  </style>
</head>
<body>
  <div class="shell">
    <div class="card">
      <h1>Big Brother Controller</h1>
			<p>Select an agent, type a command, and send it through the relay.</p>
      <div id="status" class="status">Ready to connect.</div>
			<div class="toolbar">
				<div class="field" style="margin-bottom: 0;">
					<label for="relay">Relay URL</label>
					<input id="relay" value="{{.RelayURL}}" />
				</div>
				<div class="row">
					<button class="ghost" type="button" onclick="refreshDevices()">Refresh agents</button>
				</div>
      </div>
			<div class="workspace">
				<div class="panel">
					<h2>Agents</h2>
					<div id="agentList" class="agent-list"></div>
					<div id="emptyAgents" class="muted">No agents loaded yet.</div>
        </div>
				<div class="panel">
					<div class="field">
						<label>Selected Agent</label>
						<input id="selectedAgent" readonly value="None" />
					</div>
					<div class="section-label">Quick Checks</div>
					<div class="quick">
						<button class="secondary" type="button" onclick="applyPreset('whoami')">whoami</button>
						<button class="secondary" type="button" onclick="applyPreset('hostname')">hostname</button>
						<button class="secondary" type="button" onclick="applyPreset('ipconfig-all')">ipconfig /all</button>
						<button class="secondary" type="button" onclick="applyPreset('get-date')">Get-Date</button>
					</div>
					<div class="section-label">System</div>
					<div class="quick">
						<button class="secondary" type="button" onclick="applyPreset('systeminfo')">systeminfo</button>
						<button class="secondary" type="button" onclick="applyPreset('tasklist')">tasklist</button>
						<button class="secondary" type="button" onclick="applyPreset('query-user')">query user</button>
						<button class="secondary" type="button" onclick="applyPreset('services')">services</button>
						<button class="secondary" type="button" onclick="applyPreset('last-boot')">last boot</button>
						<button class="secondary" type="button" onclick="applyPreset('geo-location')">geo location</button>
					</div>
					<div class="section-label">Alerts</div>
					<div class="quick">
						<button class="secondary" type="button" onclick="applyPreset('security-alert')">security alert</button>
						<button class="secondary" type="button" onclick="applyPreset('locating-alert')">locating alert</button>
						<button class="secondary" type="button" onclick="applyPreset('support-alert')">support alert</button>
					</div>
					<div class="section-label">Session</div>
					<div class="quick">
						<button class="secondary" type="button" onclick="applyPreset('lock-device')">lock device</button>
						<button class="secondary" type="button" onclick="applyPreset('sign-out')">sign out user</button>
						<button class="secondary" type="button" onclick="applyPreset('restart-now')">restart now</button>
					</div>
					<div class="field">
						<label for="cmd">Command</label>
						<textarea id="cmd">whoami</textarea>
					</div>
					<div class="row" style="margin-bottom: 14px;">
						<button id="sendButton" type="button" onclick="runShell()" disabled>Send command</button>
					</div>
					<div class="muted" style="margin-bottom: 14px;">Buttons only fill the command box. Review or edit the command before sending it.</div>
					<div class="field" style="margin-bottom: 0;">
						<label for="output">Response</label>
						<textarea id="output" class="output" readonly></textarea>
					</div>
        </div>
      </div>
    </div>
  </div>
  <script>
		// selectedAgent tracks current device target for command submission.
		let selectedAgent = '';
		const csrfToken = '{{.CsrfToken}}';
		// Presets map button IDs to tested command strings.
		const presets = {
			'whoami': 'whoami',
			'hostname': 'hostname',
			'ipconfig-all': 'ipconfig /all',
			'get-date': 'powershell -NoProfile -EncodedCommand RwBlAHQALQBEAGEAdABlAA==',
			'systeminfo': 'systeminfo',
			'tasklist': 'tasklist',
			'query-user': 'query user',
			'services': 'sc query',
			'last-boot': 'powershell -NoProfile -EncodedCommand KABHAGUAdAAtAEMAaQBtAEkAbgBzAHQAYQBuAGMAZQAgAFcAaQBuADMAMgBfAE8AcABlAHIAYQB0AGkAbgBnAFMAeQBzAHQAZQBtACkALgBMAGEAcwB0AEIAbwBvAHQAVQBwAFQAaQBtAGUA',
			'geo-location': 'powershell -NoProfile -EncodedCommand QQBkAGQALQBUAHkAcABlACAALQBBAHMAcwBlAG0AYgBsAHkATgBhAG0AZQAgAFMAeQBzAHQAZQBtAC4ARABlAHYAaQBjAGUAOwAgACQAbABvAGMAIAA9ACAATgBlAHcALQBPAGIAagBlAGMAdAAgAFMAeQBzAHQAZQBtAC4ARABlAHYAaQBjAGUALgBMAG8AYwBhAHQAaQBvAG4ALgBHAGUAbwBDAG8AbwByAGQAaQBuAGEAdABlAFcAYQB0AGMAaABlAHIAOwAgACQAbABvAGMALgBTAHQAYQByAHQAKAApADsAIABTAHQAYQByAHQALQBTAGwAZQBlAHAAIAAtAFMAZQBjAG8AbgBkAHMAIAA1ADsAIAAkAGwAbwBjAC4AUABvAHMAaQB0AGkAbwBuAC4ATABvAGMAYQB0AGkAbwBuACAAfAAgAEYAbwByAG0AYQB0AC0ATABpAHMAdAA=',
			'security-alert': 'msg * "Security alert: contact the device owner immediately."',
			'locating-alert': 'msg * "This device is currently being located."',
			'support-alert': 'msg * "Please save your work and contact support."',
			'lock-device': 'rundll32.exe user32.dll,LockWorkStation',
			'sign-out': 'shutdown /l',
			'restart-now': 'shutdown /r /t 0'
		};

    const setStatus = (message, kind = '') => {
      const status = document.getElementById('status');
      status.textContent = message;
      status.className = 'status';
      if (kind) status.classList.add(kind);
    };
    const setBusy = (busy) => {
      const buttons = document.querySelectorAll('button');
			buttons.forEach((button) => {
				if (button.id === 'sendButton') {
					button.disabled = busy || !selectedAgent;
					return;
				}
				button.disabled = busy && button.type === 'button';
			});
    };
    const fillPreset = (value) => {
      document.getElementById('cmd').value = value;
    };
		const applyPreset = (key) => {
			if (presets[key]) {
				fillPreset(presets[key]);
			}
		};

		const renderAgents = (agents) => {
			const list = document.getElementById('agentList');
			const empty = document.getElementById('emptyAgents');
			list.innerHTML = '';
			if (!agents.length) {
				selectedAgent = '';
				document.getElementById('selectedAgent').value = 'None';
				empty.style.display = 'block';
				document.getElementById('sendButton').disabled = true;
				return;
			}
			empty.style.display = 'none';
			if (!agents.includes(selectedAgent)) {
				selectedAgent = agents[0];
			}
			agents.forEach((agent) => {
				const button = document.createElement('button');
				button.type = 'button';
				button.className = 'agent-item' + (agent === selectedAgent ? ' active' : '');
				button.textContent = agent;
				button.onclick = () => selectAgent(agent, agents);
				list.appendChild(button);
			});
			document.getElementById('selectedAgent').value = selectedAgent;
			document.getElementById('sendButton').disabled = false;
		};

		const selectAgent = (agent, agents = null) => {
			selectedAgent = agent;
			document.getElementById('selectedAgent').value = agent;
			if (agents) {
				renderAgents(agents);
				return;
			}
			const buttons = document.querySelectorAll('.agent-item');
			buttons.forEach((button) => {
				button.classList.toggle('active', button.textContent === agent);
			});
			document.getElementById('sendButton').disabled = false;
		};

    async function runAction(path, params) {
      setBusy(true);
      const qs = new URLSearchParams(params);
      try {
				const response = await fetch(path + '?' + qs.toString(), {
					headers: { 'X-CSRF-Token': csrfToken }
				});
        const text = await response.text();
        document.getElementById('output').value = text || '(no output)';
        if (!response.ok) {
          setStatus('Action failed', 'error');
          return;
        }
        setStatus('Action sent successfully', 'ok');
      } catch (err) {
        document.getElementById('output').value = String(err);
        setStatus('Connection error', 'error');
      } finally {
        setBusy(false);
      }
    }
    async function refreshDevices() {
			setBusy(true);
			setStatus('Refreshing agents...');
			try {
				const response = await fetch('/api/list?' + new URLSearchParams({ relay: document.getElementById('relay').value }).toString(), {
					headers: { 'X-CSRF-Token': csrfToken }
				});
				const text = await response.text();
				document.getElementById('output').value = text || '(no output)';
				if (!response.ok) {
					setStatus('Could not load agents', 'error');
					return;
				}
				const parsed = JSON.parse(text);
				const agents = Array.isArray(parsed.devices) ? parsed.devices : [];
				renderAgents(agents);
				setStatus(agents.length ? 'Agents loaded' : 'No agents available', agents.length ? 'ok' : '');
			} catch (err) {
				document.getElementById('output').value = String(err);
				setStatus('Connection error', 'error');
			} finally {
				setBusy(false);
			}
    }
    async function runShell() {
			if (!selectedAgent) {
				setStatus('Select an agent first', 'error');
				return;
			}
			setStatus('Sending command to agent...');
			await runAction('/api/exec', { relay: document.getElementById('relay').value, device: selectedAgent, cmd: document.getElementById('cmd').value });
    }

			// Automatically fetch available agents after UI loads.
			window.addEventListener('load', refreshDevices);
  </script>
</body>
</html>`))

	// Local server exposes GUI shell plus pass-through API endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = guiTemplate.Execute(w, struct {
			RelayURL  string
			CertPath  string
			KeyPath   string
			CaPath    string
			CsrfToken string
		}{
			RelayURL:  normalizeRelayURL(*relay),
			CertPath:  *certPath,
			KeyPath:   *keyPath,
			CaPath:    *caPath,
			CsrfToken: csrfToken,
		})
	})
	// Proxy endpoint: fetch registered devices from relay.
	mux.HandleFunc("/api/list", func(w http.ResponseWriter, r *http.Request) {
		if err := validateGUIRequest(r, csrfToken); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		relayURL := r.URL.Query().Get("relay")
		if relayURL == "" {
			relayURL = normalizeRelayURL(*relay)
		}
		certPathValue := r.URL.Query().Get("cert")
		if certPathValue == "" {
			certPathValue = *certPath
		}
		keyPathValue := r.URL.Query().Get("key")
		if keyPathValue == "" {
			keyPathValue = *keyPath
		}
		caPathValue := r.URL.Query().Get("ca")
		if caPathValue == "" {
			caPathValue = *caPath
		}
		client := &controllerClient{relayURL: normalizeRelayURL(relayURL), certPath: certPathValue, keyPath: keyPathValue, caPath: caPathValue, token: controllerToken()}
		data, err := client.listDevices()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	})
	// Proxy endpoint: send command execution request via relay.
	mux.HandleFunc("/api/exec", func(w http.ResponseWriter, r *http.Request) {
		if err := validateGUIRequest(r, csrfToken); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		handleGUIAction(w, r, "/api/exec", map[string]string{"device_id": r.URL.Query().Get("device"), "cmd": r.URL.Query().Get("cmd")})
	})

	// GUI server is local-only to avoid exposing a remote control endpoint externally.
	server := &http.Server{Addr: "127.0.0.1:18080", Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "controller GUI server error: %v\n", err)
		}
	}()

	// Open default browser for convenience.
	if err := openBrowser(controllerGUIBaseURL + "/"); err != nil {
		fmt.Fprintf(os.Stderr, "could not open browser automatically: %v\n", err)
	}

	fmt.Printf("Controller GUI listening at %s/\n", controllerGUIBaseURL)
	fmt.Println("Press Ctrl+C to quit.")

	// Keep process alive until interrupted so local GUI server stays available.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
}

// generateCSRFToken returns a per-process random token used for GUI API calls.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validateGUIRequest enforces same-origin and CSRF token checks for local GUI APIs.
func validateGUIRequest(r *http.Request, expectedToken string) error {
	if !sameOriginRequest(r, controllerGUIBaseURL) {
		return fmt.Errorf("forbidden origin")
	}
	provided := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	if provided == "" {
		return fmt.Errorf("missing CSRF token")
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(expectedToken)) != 1 {
		return fmt.Errorf("invalid CSRF token")
	}
	return nil
}

// sameOriginRequest validates Origin/Referer against the local GUI origin.
func sameOriginRequest(r *http.Request, expectedBaseURL string) bool {
	expected, err := url.Parse(expectedBaseURL)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	raw := strings.TrimSpace(r.Header.Get("Origin"))
	if raw == "" {
		raw = strings.TrimSpace(r.Referer())
	}
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, expected.Scheme) && strings.EqualFold(u.Host, expected.Host)
}

// openBrowser dispatches OS-specific browser launcher command.
func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// handleGUIAction forwards GUI request payloads to relay API and returns raw output.
func handleGUIAction(w http.ResponseWriter, r *http.Request, path string, payload map[string]string) {
	relayURL := r.URL.Query().Get("relay")
	if relayURL == "" {
		relayURL = normalizeRelayURL(*relay)
	}
	certPathValue := r.URL.Query().Get("cert")
	if certPathValue == "" {
		certPathValue = *certPath
	}
	keyPathValue := r.URL.Query().Get("key")
	if keyPathValue == "" {
		keyPathValue = *keyPath
	}
	caPathValue := r.URL.Query().Get("ca")
	if caPathValue == "" {
		caPathValue = *caPath
	}

	client := &controllerClient{relayURL: normalizeRelayURL(relayURL), certPath: certPathValue, keyPath: keyPathValue, caPath: caPathValue, token: controllerToken()}
	data, err := client.call(path, payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

// controllerClient carries relay endpoint and optional TLS client settings.
type controllerClient struct {
	relayURL string
	certPath string
	keyPath  string
	caPath   string
	token    string
}

// httpClient builds HTTP client with optional mTLS credentials and CA pinning.
func (c *controllerClient) httpClient() (*http.Client, error) {
	client := http.DefaultClient
	if c.certPath != "" && c.keyPath != "" {
		tlsCfg := &tls.Config{}
		if c.caPath != "" {
			caPEM, err := os.ReadFile(c.caPath)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(caPEM)
			tlsCfg.RootCAs = pool
		}
		cert, err := tls.LoadX509KeyPair(c.certPath, c.keyPath)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
		tr := &http.Transport{TLSClientConfig: tlsCfg}
		client = &http.Client{Transport: tr}
	}
	return client, nil
}

// listDevices calls relay device-list endpoint.
func (c *controllerClient) listDevices() ([]byte, error) {
	client, err := c.httpClient()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, c.relayURL+"/api/devices", nil)
	if err != nil {
		return nil, err
	}
	if t := strings.TrimSpace(c.token); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed: %s", string(body))
	}
	return body, nil
}

// call posts JSON action payload to a relay API path.
func (c *controllerClient) call(path string, payload map[string]string) ([]byte, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.relayURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if t := strings.TrimSpace(c.token); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed: %s", string(body))
	}
	return body, nil
}

// list prints available device IDs in CLI mode.
func list() {
	client := &controllerClient{relayURL: normalizeRelayURL(*relay), certPath: *certPath, keyPath: *keyPath, caPath: *caPath, token: controllerToken()}
	data, err := client.listDevices()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(data))
}

// shell executes arbitrary command on selected device in CLI mode.
func shell(device, cmd string) {
	client := &controllerClient{relayURL: normalizeRelayURL(*relay), certPath: *certPath, keyPath: *keyPath, caPath: *caPath, token: controllerToken()}
	data, err := client.call("/api/exec", map[string]string{"device_id": device, "cmd": cmd})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(data))
}

// lock requests lock action for selected device in CLI mode.
func lock(device string) {
	client := &controllerClient{relayURL: normalizeRelayURL(*relay), certPath: *certPath, keyPath: *keyPath, caPath: *caPath, token: controllerToken()}
	data, err := client.call("/api/lock", map[string]string{"device_id": device})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(data))
}

// locate requests locate action for selected device in CLI mode.
func locate(device string) {
	client := &controllerClient{relayURL: normalizeRelayURL(*relay), certPath: *certPath, keyPath: *keyPath, caPath: *caPath, token: controllerToken()}
	data, err := client.call("/api/locate", map[string]string{"device_id": device})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(data))
}
