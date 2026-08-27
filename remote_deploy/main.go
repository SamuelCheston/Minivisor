package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

func main() {
	host := "192.168.1.105"
	port := "22"
	user := "root"
	pass := "114514"

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pass),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", host+":"+port, config)
	if err != nil {
		log.Fatalf("Failed to dial: %v", err)
	}
	defer client.Close()

	fmt.Println("Connected to remote host.")

	// Step 1: Install dependencies
	fmt.Println("Installing dependencies...")
	runCommand(client, "apk update && apk add bash ca-certificates shadow openssh-client")

	// Step 2: Upload binary
	fmt.Println("Uploading binary...")
	runCommand(client, "rc-service tinyvisor stop || true")
	runCommand(client, "killall tinyvisor || true")
	runCommand(client, "mkdir -p /opt/tinyvisor")
	runCommand(client, "rm -f /opt/tinyvisor/tinyvisor")
	uploadFile(client, "../build/tinyvisor", "/opt/tinyvisor/tinyvisor")
	runCommand(client, "chmod +x /opt/tinyvisor/tinyvisor")

	// Step 3: Install service
	fmt.Println("Installing service...")
	runCommand(client, "/opt/tinyvisor/tinyvisor -service-install openrc")
	runCommand(client, "chown -R tinyvisor:tinyvisor /opt/tinyvisor")
	runCommand(client, "rc-update add tinyvisor default")
	runCommand(client, "rc-service tinyvisor restart")

	// Step 4: Check status
	fmt.Println("Checking service status...")
	time.Sleep(2 * time.Second)
	runCommand(client, "rc-service tinyvisor status")
	runCommand(client, "cat /opt/tinyvisor/config.json")

	fmt.Println("Deployment complete.")
}

func runCommand(client *ssh.Client, cmd string) {
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	fmt.Printf("> %s\n", cmd)
	if err := session.Run(cmd); err != nil {
		log.Printf("Command failed: %v", err)
	}
}

func uploadFile(client *ssh.Client, localPath, remotePath string) {
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	file, err := os.Open(localPath)
	if err != nil {
		log.Fatalf("Failed to open local file: %v", err)
	}
	defer file.Close()

	session.Stdin = file
	fmt.Printf("> cat > %s\n", remotePath)
	if err := session.Run(fmt.Sprintf("cat > %s", remotePath)); err != nil {
		log.Fatalf("Failed to upload file: %v", err)
	}
}
