#!/bin/bash
set -e

echo "Building Frontend..."
cd frontend
npm install
npm run build
cd ..

echo "Building Backend..."
mkdir -p build
cd backend
go build -o ../build/tinyvisor .
cd ..

echo "Done! Binary is at ./build/tinyvisor"
