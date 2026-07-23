//go:build windows

package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/svc"
)

type agentWindowsService struct {
	cfg *Config
}

// isWindowsService reports whether process is running under the Service Control Manager.
func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// runWindowsService starts managed service lifecycle callbacks.
func runWindowsService(cfg *Config) error {
	return svc.Run(windowsServiceName, &agentWindowsService{cfg: cfg})
}

// Execute handles SCM control messages and runs agent loop in service mode.
func (s *agentWindowsService) Execute(args []string, req <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// Start runtime asynchronously so control requests can be handled concurrently.
	errCh := make(chan error, 1)
	go func() {
		if err := ensureProvisioned(s.cfg); err != nil {
			errCh <- err
			return
		}
		errCh <- runAgentRuntime(s.cfg)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case change := <-req:
			switch change.Cmd {
			case svc.Interrogate:
				changes <- change.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				return false, 0
			}
		case err := <-errCh:
			if err != nil {
				log.Printf("service runtime error: %v", err)
				return false, 1
			}
			return false, 0
		}
	}
}

// installWindowsService creates or updates service definition and starts it.
func installWindowsService(name, binPath string) error {
	output, err := runSC("create", name, "binPath=", binPath, "start=", "auto", "obj=", "LocalSystem", "type=", "own")
	if err != nil {
		if !strings.Contains(string(output), "FAILED 1073") {
			return fmt.Errorf("create service: %w: %s", err, string(output))
		}
		configOutput, configErr := runSC("config", name, "binPath=", binPath, "start=", "auto")
		if configErr != nil {
			return fmt.Errorf("update service: %w: %s", configErr, string(configOutput))
		}
	}

	// Keep normal auto-start so reconnect attempts begin immediately on boot.
	if autoOutput, autoErr := runSC("config", name, "start=", "auto"); autoErr != nil {
		return fmt.Errorf("set auto-start: %w: %s", autoErr, string(autoOutput))
	}

	// Configure automatic restart policy for resilience.
	if recoveryOutput, recoveryErr := runSC("failure", name, "reset=", "86400", "actions=", "restart/5000/restart/15000/restart/30000"); recoveryErr != nil {
		return fmt.Errorf("set recovery actions: %w: %s", recoveryErr, string(recoveryOutput))
	}
	if failureFlagOutput, failureFlagErr := runSC("failureflag", name, "1"); failureFlagErr != nil {
		return fmt.Errorf("enable recovery actions: %w: %s", failureFlagErr, string(failureFlagOutput))
	}

	_, _ = runSC("stop", name)
	startOutput, err := runSC("start", name)
	if err != nil {
		return fmt.Errorf("start service: %w: %s", err, string(startOutput))
	}
	return nil
}

// runSC wraps calls to the Windows `sc` utility.
func runSC(args ...string) ([]byte, error) {
	return exec.Command("sc", args...).CombinedOutput()
}
