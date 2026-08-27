#!/bin/bash
# Minivisor Remote Deployment Script for Alpine LXC
# Usage: ./deploy_remote.sh [ip] [user] [password]

IP=${1:-"192.168.1.105"}
USER=${2:-"root"}
PASS=${3:-"114514"}

echo "--- Step 1: Building Minivisor ---"
./build.sh

if [ ! -f "build/tinyvisor" ]; then
    echo "Error: Build failed, binary not found."
    exit 1
fi

echo "--- Step 2: Preparing Remote Environment ($IP) ---"
# Install dependencies and create user if needed
# Alpine uses 'apk' and 'adduser'
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "$USER@$IP" << EOF
    apk update
    apk add bash ca-certificates
    # The binary itself will handle user creation if run as root with -service-install
EOF

echo "--- Step 3: Uploading Binary ---"
sshpass -p "$PASS" scp -o StrictHostKeyChecking=no build/tinyvisor "$USER@$IP:/usr/local/bin/tinyvisor"

echo "--- Step 4: Installing OpenRC Service ---"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "$USER@$IP" << EOF
    chmod +x /usr/local/bin/tinyvisor
    /usr/local/bin/tinyvisor -service-install openrc
    rc-update add tinyvisor default
    rc-service tinyvisor restart
EOF

echo "--- Deployment Complete ---"
echo "Visit: http://$IP:18083"
