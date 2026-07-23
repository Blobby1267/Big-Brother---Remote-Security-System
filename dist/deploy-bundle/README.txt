Big Brother deploy bundle
=========================

This folder contains the portable files needed for the USB deployment flow:

- agent.exe: installs the agent onto a target Windows device
- config.json: a ready-to-edit template configuration file

Quick start:
1. Edit config.json with the target device_id, setup_token, relay_api_address, and relay_address.
2. Copy agent.exe and config.json to a USB drive.
3. On the target Windows machine, run:
   .\agent.exe --install
