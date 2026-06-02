#!/bin/bash
set -e

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
BUILD_DIR="dist"
LDFLAGS="-s -w -X main.version=${VERSION}"

rm -rf ${BUILD_DIR}
mkdir -p ${BUILD_DIR}

echo "Building web assets..."
cd web && npm run build && cd ..

echo "Building agent binaries..."

GOOS=linux   GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o ${BUILD_DIR}/agent-linux-amd64   ./cmd/agent
GOOS=linux   GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o ${BUILD_DIR}/agent-linux-arm64   ./cmd/agent
GOOS=darwin  GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o ${BUILD_DIR}/agent-darwin-amd64  ./cmd/agent
GOOS=darwin  GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o ${BUILD_DIR}/agent-darwin-arm64  ./cmd/agent
GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o ${BUILD_DIR}/agent-windows-amd64.exe ./cmd/agent
GOOS=windows GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o ${BUILD_DIR}/agent-windows-arm64.exe ./cmd/agent

echo ""
echo "Build complete:"
ls -lh ${BUILD_DIR}/
