package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const systemdTemplate = `[Unit]
Description=Tinyvisor Service
After=network.target

[Service]
Type=simple
User={{.User}}
WorkingDirectory={{.WorkDir}}
ExecStart={{.ExecPath}}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

const openrcTemplate = `#!/sbin/openrc-run

name="tinyvisor"
description="Tinyvisor Service"
command="{{.ExecPath}}"
command_args=""
command_user="{{.User}}"
command_background=true
pidfile="/run/${RC_SVCNAME}.pid"
working_directory="{{.WorkDir}}"

depend() {
	need net
}
`

type ServiceConfig struct {
	User     string
	WorkDir  string
	ExecPath string
}

func installService(serviceType string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}
	execPath, err = filepath.Abs(execPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute executable path: %v", err)
	}

	workDir := filepath.Dir(execPath)

	// Default to 'tinyvisor' user as requested by user decision
	user := "tinyvisor"
	checkAndCreateUser(user)

	config := ServiceConfig{
		User:     user,
		WorkDir:  workDir,
		ExecPath: execPath,
	}

	switch serviceType {
	case "systemd":
		return installSystemd(config)
	case "openrc":
		return installOpenRC(config)
	default:
		return fmt.Errorf("unsupported service type: %s", serviceType)
	}
}

func installSystemd(config ServiceConfig) error {
	tmpl, err := template.New("systemd").Parse(systemdTemplate)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return err
	}

	unitPath := "/etc/systemd/system/tinyvisor.service"
	fmt.Printf("Writing systemd unit file to %s...\n", unitPath)

	err = os.WriteFile(unitPath, buf.Bytes(), 0644)
	if err != nil {
		if os.IsPermission(err) {
			fmt.Println("\nPermission denied. Please run with sudo:")
			fmt.Printf("sudo ./tinyvisor -service-install systemd\n")
			return nil
		}
		return err
	}

	fmt.Println("Systemd unit file installed successfully.")
	fmt.Println("To enable and start the service, run:")
	fmt.Println("  sudo systemctl daemon-reload")
	fmt.Println("  sudo systemctl enable tinyvisor")
	fmt.Println("  sudo systemctl start tinyvisor")

	return nil
}

func installOpenRC(config ServiceConfig) error {
	tmpl, err := template.New("openrc").Parse(openrcTemplate)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return err
	}

	initPath := "/etc/init.d/tinyvisor"
	fmt.Printf("Writing OpenRC init file to %s...\n", initPath)

	err = os.WriteFile(initPath, buf.Bytes(), 0755)
	if err != nil {
		if os.IsPermission(err) {
			fmt.Println("\nPermission denied. Please run with sudo:")
			fmt.Printf("sudo ./tinyvisor -service-install openrc\n")
			return nil
		}
		return err
	}

	fmt.Println("OpenRC init file installed successfully.")
	fmt.Println("To enable and start the service, run:")
	fmt.Println("  sudo rc-update add tinyvisor default")
	fmt.Println("  sudo rc-service tinyvisor start")

	return nil
}

func checkAndCreateUser(username string) {
	_, err := exec.LookPath("id")
	if err != nil {
		return
	}

	err = exec.Command("id", "-u", username).Run()
	if err == nil {
		fmt.Printf("User '%s' already exists.\n", username)
		return
	}

	fmt.Printf("User '%s' does not exist. You might need to create it:\n", username)
	fmt.Printf("  sudo useradd -r -s /bin/false %s\n", username)
}

type ServiceStatus struct {
	Type       string `json:"type"`
	Installed  bool   `json:"installed"`
	UserExists bool   `json:"userExists"`
	UnitPath   string `json:"unitPath"`
	CanInstall bool   `json:"canInstall"`
}

func getServiceStatus() ServiceStatus {
	status := ServiceStatus{
		Type:       "none",
		Installed:  false,
		UserExists: false,
		CanInstall: false,
	}

	// Check user
	err := exec.Command("id", "-u", "tinyvisor").Run()
	status.UserExists = (err == nil)

	// Check systemd
	if _, err := exec.LookPath("systemctl"); err == nil {
		status.Type = "systemd"
		status.UnitPath = "/etc/systemd/system/tinyvisor.service"
		if _, err := os.Stat(status.UnitPath); err == nil {
			status.Installed = true
		}
		status.CanInstall = true
	} else if _, err := exec.LookPath("rc-service"); err == nil {
		status.Type = "openrc"
		status.UnitPath = "/etc/init.d/tinyvisor"
		if _, err := os.Stat(status.UnitPath); err == nil {
			status.Installed = true
		}
		status.CanInstall = true
	}

	return status
}
