# Big-Brother - Remote Security System

Last Updated: 2026-07-08

A self-hosted remote device recovery/control system built in Go.

Purpose:
- Manage only your own devices (or devices you are explicitly authorized to administer).
- Keep managed endpoints behind NAT/firewalls by using outbound-only agent connections.
- Execute remote commands, lock sessions, and run locate workflows through a single relay.

## Legal And Safety Notice

This project is for authorized defensive administration only.
Do not deploy this software on devices you do not own or do not have explicit permission to manage.

## System Overview

The project has three executables:
- Agent: Runs on each managed endpoint and keeps a persistent outbound TLS session to relay.
- Relay: Central broker/API server that authenticates agent identity and routes controller requests.
- Controller: Operator-side CLI + local web GUI for listing devices and sending actions.

High-level flow:
1. Agent provisions once with a one-time setup token and its generated public key.
2. Relay stores device_id -> public_key mapping and invalidates the setup token.
3. Agent reconnects over TLS and authenticates with a signed timestamp payload.
4. Controller sends API requests to relay.
5. Relay forwards request to the matching live agent session and returns the response.

## Architecture And Data Paths

Provisioning path:
- Agent -> relay HTTP API: /api/provision
- Uses config setup_token and public key registration

Runtime command path:
- Controller -> relay HTTP API: /api/exec, /api/lock, /api/locate, /api/devices
- Relay -> agent TLS socket: JSON wire messages (auth/exec/resp/ping/pong)

Persistence path:
- Relay stores provisioned devices in relay-devices.json (or RELAY_DEVICE_STORE_FILE path)

## Repository File Map

Workspace and CI:
- [go.work](go.work): Go workspace linking agent, controller, relay modules.
- [go.work.sum](go.work.sum): Workspace dependency checksum lockfile (machine-managed).
- [.github/workflows/go.yml](.github/workflows/go.yml): CI workflow running tests + build script.

Agent module:
- [agent/go.mod](agent/go.mod): Agent module metadata/dependencies.
- [agent/config.go](agent/config.go): Config schema + JSON loader.
- [agent/main.go](agent/main.go): Entrypoint; install/provision/runtime routing.
- [agent/provision.go](agent/provision.go): Key generation/storage + provisioning HTTP registration.
- [agent/runtime.go](agent/runtime.go): TLS runtime loop, auth handshake, command execution.
- [agent/service_windows.go](agent/service_windows.go): Windows service host/SCM integration.
- [agent/service_stub.go](agent/service_stub.go): Non-Windows stubs for service functions.
- [agent/README.md](agent/README.md): Agent-focused quick notes.

Controller module:
- [controller/go.mod](controller/go.mod): Controller module metadata.
- [controller/go.sum](controller/go.sum): Controller dependency checksums (machine-managed).
- [controller/main.go](controller/main.go): CLI commands, local GUI server/UI, relay client.

Relay module:
- [relay/go.mod](relay/go.mod): Relay module metadata.
- [relay/main.go](relay/main.go): Provisioning API, command APIs, TLS listener, auth/session routing.

Scripts:
- [scripts/build.sh](scripts/build.sh): Unix cross-compile matrix for agent/controller/relay.
- [scripts/build.ps1](scripts/build.ps1): PowerShell cross-compile matrix.
- [scripts/build-relay-pi.sh](scripts/build-relay-pi.sh): Linux arm64 relay build helper (Pi target).
- [scripts/gen-certs.sh](scripts/gen-certs.sh): Generate CA/server/client cert material for TLS testing.
- [scripts/install-systemd.sh](scripts/install-systemd.sh): Install agent as Linux systemd service.
- [scripts/install-launchd.sh](scripts/install-launchd.sh): Install agent as macOS launch agent.
- [scripts/install-windows.ps1](scripts/install-windows.ps1): Install/update Windows agent service.
- [scripts/install-controller-windows.ps1](scripts/install-controller-windows.ps1): Install controller + desktop shortcut.
- [scripts/create-deploy-bundle.ps1](scripts/create-deploy-bundle.ps1): Create cleaned USB-ready deploy bundle.
- [scripts/config-template.json](scripts/config-template.json): Agent config template (with inline JSON comment fields).

Distribution output:
- [dist](dist): Built binaries and deployment bundle artifacts.
- [dist/deploy-bundle/README.txt](dist/deploy-bundle/README.txt): USB bundle quick-start note.

## Build And Test

From repository root:

Run all module tests:

```sh
go test ./agent ./controller ./relay
```

Unix/macOS build matrix:

```sh
./scripts/build.sh
```

Windows PowerShell build matrix:

```powershell
.\scripts\build.ps1
```

Raspberry Pi relay build:

```sh
./scripts/build-relay-pi.sh
```

## USB Deployment Workflow (Manual Config Editing)

1. Build Windows binaries to dist.
2. Generate bundle:

```powershell
./scripts/create-deploy-bundle.ps1
```

3. Edit [dist/deploy-bundle/config.json](dist/deploy-bundle/config.json) for target device.
4. Copy bundle files to USB.
5. On target Windows device, run:

```powershell
.\agent.exe --install
```

6. On operator machine, run controller executable and connect to relay.

## Required Config Fields

In config.json:
- device_id: Unique ID for managed endpoint.
- setup_token: One-time provisioning token matching relay token config.
- relay_api_address: HTTP endpoint for provisioning (typically port 8080).
- relay_address: TLS endpoint for runtime agent connection (typically port 8443).
- allow_insecure_relay: Testing-only bypass for TLS cert validation.


## Operations Commands

This section contains copy-paste commands for operating relay, controller, and agents.

### Relay Ops (Raspberry Pi / Debian)

Check relay service status:

```bash
sudo systemctl status big-brother-relay --no-pager
```

Restart relay:

```bash
sudo systemctl restart big-brother-relay
```

Reload unit files + restart relay:

```bash
sudo systemctl daemon-reload
sudo systemctl restart big-brother-relay
```

Watch relay logs live:

```bash
sudo journalctl -u big-brother-relay -f
```

View recent relay logs:

```bash
sudo journalctl -u big-brother-relay -n 120 --no-pager
```

Stop/start relay:

```bash
sudo systemctl stop big-brother-relay
sudo systemctl start big-brother-relay
```

### Setup Token Management (Raspberry Pi)

View setup tokens file:

```bash
sudo cat /etc/big-brother/setup-tokens.json
```

Validate setup tokens JSON:

```bash
sudo python3 -m json.tool /etc/big-brother/setup-tokens.json
```

Replace setup tokens file with known values:

```bash
sudo tee /etc/big-brother/setup-tokens.json > /dev/null <<'EOF'
{
	"DEVICE_ID_1": "REPLACE_WITH_ONE_TIME_TOKEN_1",
	"DEVICE_ID_2": "REPLACE_WITH_ONE_TIME_TOKEN_2"
}
EOF
sudo chmod 600 /etc/big-brother/setup-tokens.json
sudo systemctl restart big-brother-relay
```

Add/update one token without overwriting others:

```bash
sudo python3 - <<'PY'
import json
path = "/etc/big-brother/setup-tokens.json"
device_id = "Device_ID"
token = "Setup_Token"
with open(path, "r", encoding="utf-8") as f:
	data = json.load(f)
data[device_id] = token
with open(path, "w", encoding="utf-8") as f:
	json.dump(data, f, indent=2)
print("Updated", device_id)
PY
sudo chmod 600 /etc/big-brother/setup-tokens.json
sudo systemctl restart big-brother-relay
```

Clear all setup tokens (disable new provisioning until re-added):

```bash
printf "{}\n" | sudo tee /etc/big-brother/setup-tokens.json > /dev/null
sudo chmod 600 /etc/big-brother/setup-tokens.json
sudo systemctl restart big-brother-relay
```

### Controller Token Management (Raspberry Pi + Windows)

View relay env (including controller token setting):

```bash
sudo cat /etc/big-brother/relay.env
```

Set relay controller token in env file:

```bash
sudo sed -i 's|^RELAY_CONTROLLER_TOKEN=.*|RELAY_CONTROLLER_TOKEN=CHANGE_ME_STRONG_TOKEN|' /etc/big-brother/relay.env
sudo systemctl restart big-brother-relay
```

Generate a strong relay controller token:

```bash
NEW_TOKEN="$(openssl rand -base64 48 | tr -d '\n')"
echo "$NEW_TOKEN"
sudo sed -i "s|^RELAY_CONTROLLER_TOKEN=.*|RELAY_CONTROLLER_TOKEN=$NEW_TOKEN|" /etc/big-brother/relay.env
sudo systemctl restart big-brother-relay
```

Set controller token on Windows operator machine:

```powershell
setx BIGBROTHER_CONTROLLER_TOKEN "CHANGE_ME_TO_RELAY_TOKEN"
```

View controller token in current PowerShell session:

```powershell
$env:BIGBROTHER_CONTROLLER_TOKEN
```

### Device Registry Management (Raspberry Pi)

View provisioned device key registry:

```bash
sudo cat /var/lib/big-brother/relay-devices.json
```

Validate device registry JSON:

```bash
sudo python3 -m json.tool /var/lib/big-brother/relay-devices.json
```

Remove one device key mapping (forces reprovision):

```bash
sudo python3 - <<'PY'
import json, os
path = "/var/lib/big-brother/relay-devices.json"
device_id = "DEVICE_ID_PLACEHOLDER"
data = {}
if os.path.exists(path):
	with open(path, "r", encoding="utf-8") as f:
		data = json.load(f)
data.pop(device_id, None)
with open(path, "w", encoding="utf-8") as f:
	json.dump(data, f, indent=2)
print("Removed", device_id)
PY
sudo chmod 600 /var/lib/big-brother/relay-devices.json
sudo systemctl restart big-brother-relay
```

### Relay API Quick Checks (Raspberry Pi)

Verify controller auth is enforced:

```bash
curl -i http://127.0.0.1:8080/api/devices
curl -i -H "Authorization: Bearer CHANGE_ME_TO_RELAY_TOKEN" http://127.0.0.1:8080/api/devices
```

### Controller Commands (Windows)

List devices:

```powershell
.\dist\controller.exe --relay http://RELAY_HOST_OR_IP:8080 list
```

Run remote command:

```powershell
.\dist\controller.exe --relay http://RELAY_HOST_OR_IP:8080 exec DEVICE_ID_PLACEHOLDER "whoami"
```

Launch controller GUI:

```powershell
.\dist\controller.exe --relay http://RELAY_HOST_OR_IP:8080
```

### Agent Commands (Windows Target)

Install agent service (from folder containing `agent.exe` and `config.json`):

```powershell
.\agent.exe --install
```

Manual one-time provisioning test:

```powershell
.\agent.exe --config .\config.json --provision
```

Run agent in foreground for debugging:

```powershell
.\agent.exe --config .\config.json
```

Restart/check Windows service:

```powershell
Restart-Service -Name BigBrotherAgent
Get-Service -Name BigBrotherAgent
sc.exe query BigBrotherAgent
```

### Build + Bundle Commands (Windows)

Run tests:

```powershell
go test ./agent ./controller ./relay
```

Build cross-platform binaries:

```powershell
.\scripts\build.ps1
```

Build canonical Windows binaries:

```powershell
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
$env:GOOS='windows'
$env:GOARCH='amd64'
go build -o .\dist\agent.exe .\agent
go build -o .\dist\controller.exe .\controller
go build -o .\dist\relay.exe .\relay
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
```

Regenerate deploy bundle:

```powershell
.\scripts\create-deploy-bundle.ps1
```

### Common Recovery Commands

Reset one device for clean reprovision (relay side):

```bash
sudo python3 - <<'PY'
import json, os
device_id = "DEVICE_ID_PLACEHOLDER"
dev_file = "/var/lib/big-brother/relay-devices.json"
tok_file = "/etc/big-brother/setup-tokens.json"
token = "REPLACE_WITH_ONE_TIME_TOKEN"

devices = {}
if os.path.exists(dev_file):
	with open(dev_file, "r", encoding="utf-8") as f:
		devices = json.load(f)
devices.pop(device_id, None)
with open(dev_file, "w", encoding="utf-8") as f:
	json.dump(devices, f, indent=2)

tokens = {}
if os.path.exists(tok_file):
	with open(tok_file, "r", encoding="utf-8") as f:
		tokens = json.load(f)
tokens[device_id] = token
with open(tok_file, "w", encoding="utf-8") as f:
	json.dump(tokens, f, indent=2)

print("Reset complete for", device_id)
PY
sudo chmod 600 /var/lib/big-brother/relay-devices.json /etc/big-brother/setup-tokens.json
sudo systemctl restart big-brother-relay
```

## Controller Authentication

Relay control APIs now require a bearer token.

Relay side:
- Set `RELAY_CONTROLLER_TOKEN` to a strong random value before starting relay.

Controller side:
- Pass `--token <value>` to controller CLI, or set `BIGBROTHER_CONTROLLER_TOKEN`.
- Controller includes `Authorization: Bearer <token>` on relay API requests.

## How Files Work Together

Startup:
1. Agent loads config via [agent/config.go](agent/config.go).
2. Agent entry logic in [agent/main.go](agent/main.go) decides install/provision/runtime mode.
3. Provisioning logic in [agent/provision.go](agent/provision.go) registers public key with relay.
4. Runtime loop in [agent/runtime.go](agent/runtime.go) maintains authenticated TLS session.

Execution:
1. Controller APIs in [controller/main.go](controller/main.go) send JSON actions to relay.
2. Relay handlers in [relay/main.go](relay/main.go) resolve active agent session.
3. Relay sends wire request, agent executes command, relay returns response.

Service hosting:
- Windows service host implemented in [agent/service_windows.go](agent/service_windows.go).
- Non-Windows builds keep stubs in [agent/service_stub.go](agent/service_stub.go).
- Platform install automation handled by scripts in [scripts](scripts).

## Notes About Non-Commentable Files

Some files in this repo are intentionally machine-managed and should not be hand-commented:
- Checksum lock files: [go.work.sum](go.work.sum), [controller/go.sum](controller/go.sum)
- Binary outputs: files in [dist](dist) ending in .exe

For those files, behavior and purpose are documented in this README instead of inline comments.

## Current Status

Project is functional for personal/lab use and iterative hardening is ongoing.
Primary tested flow:
- Build binaries
- Provision endpoint through setup token
- Agent reconnect/authenticate after reboot
- Controller issues remote commands through relay

## Security

This section documents a white-box security review of the full repository as it exists in a public GitHub context.

### Security Scope Reviewed

Reviewed files and surfaces:
- Docs and metadata: `README.md`, `agent/README.md`, `go.work`, `go.work.sum`, `.github/workflows/go.yml`, `agent/go.mod`, `controller/go.mod`, `relay/go.mod`, `controller/go.sum`.
- Agent code: `agent/config.go`, `agent/main.go`, `agent/provision.go`, `agent/runtime.go`, `agent/service_stub.go`, `agent/service_windows.go`.
- Relay code: `relay/main.go`.
- Controller code: `controller/main.go`.
- Scripts and install tooling: all files under `scripts/`.
- Deployment bundle text/config artifacts: `dist/deploy-bundle/README.txt`, `dist/deploy-bundle/config.json`.

Not reviewed line-by-line as source code:
- Binary artifacts in `dist/*.exe` and `dist/deploy-bundle/*.exe` (compiled outputs).

### Attack Surface And Existing Controls

1. Agent runtime TLS channel (`agent/runtime.go` -> relay TLS listener)
- Current control: TLS is used for persistent runtime transport.
- Current control: Agent signs `device_id|timestamp` with Ed25519; relay verifies signature and a ±2 minute freshness window.
- Current control: Relay checks that authenticated `device_id` matches stored provisioned public key.

2. Provisioning channel (`agent/provision.go` -> `/api/provision`)
- Current control: One-time setup token validation and invalidation after successful provisioning.
- Current control: Device public key persisted by relay for future runtime auth binding.

3. Private key at rest on endpoint (`agent/provision.go`)
- Current control: OS keyring is preferred storage path.
- Current control: File fallback exists only when explicitly enabled via `BIGBROTHER_ALLOW_KEYFILE_FALLBACK=1`.
- Current control: Fallback file mode is restrictive (`0600`) when created.

4. Controller local GUI server (`controller/main.go`)
- Current control: GUI listener is bound to loopback (`127.0.0.1:18080`) rather than wildcard host.

5. Relay persistent state (`relay/main.go`)
- Current control: Provisioned device key map persisted with restrictive file mode (`0600`).

6. Optional mutual TLS (relay + agent/controller)
- Current control: Relay can require and verify client certificates (`RELAY_CLIENT_CA`).
- Current control: Agent/controller can present client cert/key when configured.

### Security Iteration 1: Located Vulnerabilities And Hardening Actions
Severity legend: Critical, High, Medium, Low.

This is Security Iteration 1.
After going through the code looking for vulnerabilities these are the high-priority vulnerabilities I have found.
Next iteration goal: implement fixes for Critical/High items first, then rerun white-box review and update this section.


1. Critical: Unauthenticated relay control APIs allow remote command execution
- Location: `relay/main.go` (`/api/exec`, `/api/lock`, `/api/locate`, `/api/devices`).
- Why vulnerable: No controller authentication or authorization checks are enforced on HTTP API endpoints.
- Impact: Any network party that can reach relay HTTP port can list devices and issue commands.
- Status: Mitigated in code by mandatory bearer-token authentication on relay control endpoints.


2. High: Provisioning transmits setup token over plaintext HTTP by default
- Location: `agent/provision.go` (`provisionEndpoint` default `http://<host>:8080`) and docs/templates.
- Why vulnerable: Setup token and public key registration travel over plaintext unless operator manually deploys HTTPS proxy or secure network controls.
- Impact: Token interception and malicious device registration on untrusted networks.
- Status: Partially mitigated; HTTPS-first logic exists, but current deployment integration keeps HTTP provisioning enabled for zero-touch target installs.


3. High: Insecure TLS mode is easy to enable and currently defaulted in templates
- Location: `agent/runtime.go` (`InsecureSkipVerify` path), `scripts/config-template.json`, `dist/deploy-bundle/config.json`.
- Why vulnerable: `allow_insecure_relay` is set `true` in shipped templates/bundle and env var bypass exists.
- Impact: MITM risk on runtime channel; relay impersonation possible.
- Status: Partially mitigated; runtime guardrails were improved, but current deploy-template defaults intentionally keep `allow_insecure_relay: true` for compatibility.


4. High: Controller GUI endpoints are vulnerable to localhost CSRF-style abuse
- Location: `controller/main.go` (`/api/exec`, `/api/list`).
- Why vulnerable: Browser-accessible local endpoints accept command requests without CSRF token/origin validation.
- Impact: A malicious web page viewed by operator could trigger requests to `127.0.0.1:18080` and execute commands via active controller session.
- Status: Mitigated in code via per-process CSRF tokens and same-origin validation.


5. Medium: Static request ID (`"1"`) risks response confusion across concurrent requests
- Location: `relay/main.go` (`handleExec`, `handleSimpleAction`).
- Why vulnerable: In-flight response mapping uses request IDs; constant ID can cause collisions and misassociation under concurrency.
- Impact: Command/result integrity issues; potential cross-request data mix-up.


6. Medium: Relay HTTP server lacks defensive timeouts
- Location: `relay/main.go` (`http.ListenAndServe` with default server).
- Why vulnerable: No read header/body/write/idle timeouts on public API server.
- Impact: Greater susceptibility to slowloris/resource exhaustion attacks.


7. Medium: TLS hardening parameters are not explicitly pinned
- Location: `relay/main.go` and `agent/runtime.go` TLS configs.
- Why vulnerable: No explicit `MinVersion` policy; security posture depends on runtime defaults.
- Impact: Policy drift risk and weaker-than-expected protocol negotiation in some environments.


8. Medium: Public repository contains real-looking setup token in templates/artifacts
- Location: `scripts/config-template.json`, `dist/deploy-bundle/config.json` (`"setup_token": "REPLACE_WITH_ONE_TIME_TOKEN"`).
- Why vulnerable: Publicly visible token-shaped values normalize insecure reuse and can become real credential exposure if reused operationally.
- Impact: Unauthorized provisioning if reused in deployed environments.
- Status: Not fully mitigated in deployment artifacts; `dist/deploy-bundle/config.json` can contain active token values during operational prep.


9. Medium: Device ID is not sanitized before fallback key file path construction
- Location: `agent/provision.go` (`writeKeyFile`, `readKeyFile`).
- Why vulnerable: `device_id` is used in filename composition directly; path separators/special names can produce unintended paths.
- Impact: Potential path traversal or unexpected file access when fallback is enabled.


10. Low: Provisioning HTTP client has no explicit timeout
- Location: `agent/provision.go` (`http.Post`).
- Why vulnerable: Default client without timeout may hang on network stalls.
- Impact: Agent startup/provisioning can block longer than expected.


11. Low: Install flow copies config with permissive mode in helper
- Location: `agent/main.go` (`copyFile` uses mode `0755` for all copied files).
- Why vulnerable: Shared helper applies executable/world-readable bits to config files.
- Impact: Unnecessary local disclosure risk for tokens/settings on platforms honoring POSIX modes.

### Security Iteration 1 Solutions

This section tracks fixes already implemented during Security Iteration 1.

1. Solution for Critical #1: Unauthenticated relay control APIs
- Objective: prevent unauthorized remote users from calling relay control endpoints.
- Relay enforcement implemented in `relay/main.go`:
	- Added required `RELAY_CONTROLLER_TOKEN` configuration at startup.
	- Relay now fails fast on startup when `RELAY_CONTROLLER_TOKEN` is missing.
	- Added `authorizeControllerRequest(...)` middleware-style validation for control routes.
	- Enforced `Authorization: Bearer <token>` for `/api/devices`, `/api/exec`, `/api/lock`, and `/api/locate`.
	- Added constant-time token comparison to reduce token-check side-channel leakage.
- Controller compatibility implemented in `controller/main.go`:
	- Added `--token` flag support for CLI usage.
	- Added `BIGBROTHER_CONTROLLER_TOKEN` environment fallback.
	- Updated relay requests (CLI and GUI proxy path) to include bearer token when configured.
- Documentation updates:
	- Added `RELAY_CONTROLLER_TOKEN` to relay environment variable documentation.
	- Added a controller authentication usage section describing `--token` and `BIGBROTHER_CONTROLLER_TOKEN`.
- Validation performed:
	- `go test ./agent ./controller ./relay` completed successfully after the changes.
- Current result:
	- Critical #1 is mitigated in code, with relay control APIs requiring valid controller authentication.

2. Solution for High #2: Provisioning transmits setup token over plaintext HTTP by default
- Objective: ensure provisioning traffic is encrypted and prevent accidental plaintext token exposure.
- Agent provisioning hardening implemented in `agent/provision.go`:
	- Added HTTPS-first endpoint handling and explicit request timeout.
	- Added controlled insecure-provisioning path and retry fallback support for environments where relay provisioning is still HTTP on port 8080.
- Configuration hardening:
	- Deploy templates currently use HTTP provisioning endpoint to support zero-touch install workflows.
- Current result:
	- Full mitigation is deferred by current integration requirements. Provisioning confidentiality is still reduced on untrusted networks.

3. Solution for High #3: Insecure TLS mode is easy to enable and currently defaulted in templates
- Objective: prevent accidental insecure runtime TLS posture in normal deployments.
- Runtime hardening implemented in `agent/runtime.go`:
	- Added warnings and additional runtime checks around insecure modes.
- Template/default hardening:
	- Current integration intentionally keeps `allow_insecure_relay` enabled in deploy-facing config to avoid manual CA setup on target machines.
- Current result:
	- Full mitigation is deferred by current integration requirements. Runtime MITM risk remains where certificate validation is bypassed.

4. Solution for High #4: Controller GUI localhost CSRF-style abuse
- Objective: prevent untrusted web pages from triggering local controller API actions.
- GUI API hardening implemented in `controller/main.go`:
	- Added per-process random CSRF token generation (`generateCSRFToken`).
	- Embedded token in the served GUI page and attached token header (`X-CSRF-Token`) on `/api/list` and `/api/exec` browser requests.
	- Added server-side CSRF token validation using constant-time comparison.
	- Added origin/referer same-origin validation for GUI API endpoints (`validateGUIRequest` / `sameOriginRequest`).
	- Requests failing origin or CSRF checks are rejected with HTTP 403.
- Current result:
	- High #4 is mitigated in code by requiring both same-origin context and a valid CSRF token for local GUI API calls.

### Security Iteration 1 Integration Constraints And Residual Risk

The following items are not fully closed because the current deployment model prioritizes one-command target installation (`.\agent.exe --install`) with minimal operator setup:

1. High #2 remains partially open:
- Provisioning still commonly uses HTTP (`relay_api_address` on port 8080) in deploy artifacts.
- Setup tokens may traverse local networks without transport encryption.

2. High #3 remains partially open:
- Deploy-facing config keeps `allow_insecure_relay: true` to avoid CA distribution/setup on endpoints.
- Agent runtime certificate verification may be bypassed in those deployments.

3. Medium #8 remains partially open in operational artifacts:
- `dist/deploy-bundle/config.json` can contain real setup token values during deployment preparation.

Recommended next hardening step after rollout stabilization:
- Move provisioning to HTTPS with trusted CA verification.
- Default `allow_insecure_relay` to `false` in all distributed templates.
- Keep only placeholder token values in repository-tracked config artifacts.


## Disclaimer

Use only on systems you own or are explicitly authorized to administer.
Unauthorized deployment or remote control is illegal and unethical.
