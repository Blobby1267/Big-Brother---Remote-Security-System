# Agent

This folder contains the managed endpoint process for the Big-Brother project.

## What The Agent Does

- Loads per-device configuration from config.json.
- Ensures key material exists (provisions automatically when needed).
- Maintains outbound TLS connection to relay.
- Authenticates with signed timestamp payload.
- Executes relay-issued shell commands and returns output/exit code.
- On Windows, can run as a proper Windows Service.

## File Roles

- config.go: Config schema and config loader.
- main.go: Entrypoint and mode routing (install/provision/runtime).
- provision.go: Device key generation, secure key storage, provisioning API registration.
- runtime.go: TLS connect/reconnect loop, wire protocol handling, command execution.
- service_windows.go: Windows service host and SCM integration.
- service_stub.go: Non-Windows placeholders for service functions.

Usage:

Build the agent from the repository root:

```sh
go build ./agent
```

One-time provisioning (run on target device after copying `config.json` from USB):

```sh
./agent --config config.json --provision
```

Notes:
- Provisioning uses HTTP API endpoint (relay_api_address + /api/provision), while runtime uses relay_address over TLS.
- For production, use trusted certificates and avoid insecure relay mode.
- Keyring storage is preferred; file fallback requires explicit enablement.

Run the agent runtime (connects outbound to relay):

```sh
./agent --config $HOME/.config/big-brother/config.json
```

The runtime will attempt a TLS connection to the relay and perform a
lightweight JSON handshake. This scaffold currently uses permissive TLS
verification for ease of local testing — do not use that in production.
