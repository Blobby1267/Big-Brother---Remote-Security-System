package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

const windowsServiceName = "BigBrotherAgent"

var (
	// Function variables enable easier substitution in tests/mocks.
	loadStoredPrivateKey = loadPrivateKey
	provisionDevice      = Provision
)

// main routes the process into install, provisioning, service-host, or runtime mode.
func main() {
	configPath := flag.String("config", "config.json", "path to device config (from USB)")
	doProvision := flag.Bool("provision", false, "run one-time provisioning")
	installMode := flag.Bool("install", false, "install the agent as a background service and exit")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Installation mode copies binaries/config and creates or updates Windows service.
	if *installMode {
		if err := installAgent(*configPath); err != nil {
			log.Fatalf("install failed: %v", err)
		}
		fmt.Println("Installation completed. The agent is now running in the background.")
		return
	}

	// Service context detection ensures Windows service lifecycle hooks are honored.
	if runtime.GOOS == "windows" {
		isSvc, err := isWindowsService()
		if err != nil {
			log.Fatalf("detect windows service: %v", err)
		}
		if isSvc {
			if err := runWindowsService(cfg); err != nil {
				log.Fatalf("run windows service: %v", err)
			}
			return
		}
	}

	// Explicit one-shot provisioning mode for manual setup/debug workflows.
	if *doProvision {
		if err := provisionDevice(cfg); err != nil {
			log.Fatalf("provision failed: %v", err)
		}
		fmt.Println("Provisioning completed.")
		return
	}

	// Normal startup path auto-provisions if key material is missing.
	if err := ensureProvisioned(cfg); err != nil {
		log.Fatalf("provisioning failed: %v", err)
	}

	// Runtime loop maintains relay connection and handles remote commands.
	if err := runAgentRuntime(cfg); err != nil {
		log.Fatalf("agent runtime error: %v", err)
	}
}

// ensureProvisioned checks for existing private key and provisions only when required.
func ensureProvisioned(cfg *Config) error {
	if _, err := loadStoredPrivateKey(cfg.DeviceID); err == nil {
		return nil
	}
	fmt.Fprintln(os.Stderr, "no existing private key found; provisioning device")
	return provisionDevice(cfg)
}

// installAgent performs local Windows installation and registers startup service.
func installAgent(configPath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("USB install is currently implemented for Windows only")
	}
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	installDir := filepath.Join(os.Getenv("ProgramFiles"), "BigBrother")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("mkdir install dir: %w", err)
	}
	targetPath := filepath.Join(installDir, "agent.exe")
	if err := copyFile(binPath, targetPath); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}
	if err := copyFile(configPath, filepath.Join(installDir, "config.json")); err != nil {
		return fmt.Errorf("copy config: %w", err)
	}
	// Service command points the installed agent at the installed config path.
	cmd := fmt.Sprintf("\"%s\" --config \"%s\"", targetPath, filepath.Join(installDir, "config.json"))
	if err := installWindowsService("BigBrotherAgent", cmd); err != nil {
		return err
	}
	return nil
}

// copyFile is a small helper used by installer workflow.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
