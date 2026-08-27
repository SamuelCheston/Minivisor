#!/bin/bash
# Minivisor Remote Deployment Script for Alpine LXC
# Usage: ./deploy_remote.sh [ip] [user] [password]

IP=${1:-"192.168.1.133"}
USER=${2:-"root"}
PASS=${3:-"114514"}

echo "--- Step 1: Building Minivisor ---"
./build.sh

if [ ! -f "build/tinyvisor" ]; then
    echo "Error: Build failed, binary not found."
    exit 1
fi

echo "--- Step 2: Preparing Remote Environment ($IP) ---"
# Alpine needs bash, ca-certificates and shadow (for user management)
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "$USER@$IP" << EOF
    apk update
    apk add bash ca-certificates shadow
    mkdir -p /opt/tinyvisor
EOF

echo "--- Step 3: Uploading Binary ---"
sshpass -p "$PASS" scp -o StrictHostKeyChecking=no build/tinyvisor "$USER@$IP:/opt/tinyvisor/tinyvisor"

echo "--- Step 4: Installing OpenRC Service ---"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "$USER@$IP" << EOF
    chmod +x /opt/tinyvisor/tinyvisor
    # Run from the directory to ensure config.json is created there
    cd /opt/tinyvisor
    ./tinyvisor -service-install openrc
    
    # Ensure tinyvisor user owns the directory
    chown -R tinyvisor:tinyvisor /opt/tinyvisor
    
    rc-update add tinyvisor default
    rc-service tinyvisor restart
EOF

echo "--- Deployment Complete ---"
echo "Visit: http://$IP:7891"
