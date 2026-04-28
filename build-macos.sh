#!/bin/bash
set -e

NAME="agent-tools-cli"
VERSION="${VERSION:-0.0.1}"
BUILD_DIR=$(mktemp -d)
PKG_ROOT="${BUILD_DIR}/root"

echo "==> Building universal binary..."
cd "$(dirname "$0")"

CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o "${BUILD_DIR}/${NAME}-amd64" .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o "${BUILD_DIR}/${NAME}-arm64" .
lipo -create -output "${BUILD_DIR}/${NAME}" "${BUILD_DIR}/${NAME}-amd64" "${BUILD_DIR}/${NAME}-arm64"

echo "==> Preparing package root..."
mkdir -p "${PKG_ROOT}/usr/local/bin"
mkdir -p "${PKG_ROOT}/etc/agent-tools"
cp "${BUILD_DIR}/${NAME}" "${PKG_ROOT}/usr/local/bin/${NAME}"
cp config-example.yaml "${PKG_ROOT}/etc/agent-tools/config-example.yaml"
chmod 755 "${PKG_ROOT}/usr/local/bin/${NAME}"

echo "==> Building .pkg..."
pkgbuild \
  --root "${PKG_ROOT}" \
  --identifier "com.agent-tools.cli" \
  --version "${VERSION}" \
  --install-location "/" \
  "${NAME}-${VERSION}-macos.pkg"

echo ""
echo "==> Done: ${NAME}-${VERSION}-macos.pkg"

rm -rf "${BUILD_DIR}"
