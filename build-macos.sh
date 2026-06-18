#!/bin/bash
set -e

NAME="agent-tools-cli"
VERSION="${VERSION:-0.0.1}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --name)
      NAME="${2:-}"
      shift 2
      ;;
    --name=*)
      NAME="${1#*=}"
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--name <binary-and-package-name>]"
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$NAME" || ! "$NAME" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
  echo "invalid --name: ${NAME}" >&2
  echo "name must start with a letter or number and contain only letters, numbers, dot, underscore, or hyphen" >&2
  exit 1
fi

BUILD_DIR=$(mktemp -d)
PKG_ROOT="${BUILD_DIR}/root"
CONFIG_DIR="${NAME}"
LD_FLAGS="-s -w -X agent-tools/config.AppName=${NAME}"
PKG_IDENTIFIER="com.${NAME}.cli"

echo "==> Building universal binary..."
cd "$(dirname "$0")"

CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="${LD_FLAGS}" -o "${BUILD_DIR}/${NAME}-amd64" .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="${LD_FLAGS}" -o "${BUILD_DIR}/${NAME}-arm64" .
lipo -create -output "${BUILD_DIR}/${NAME}" "${BUILD_DIR}/${NAME}-amd64" "${BUILD_DIR}/${NAME}-arm64"

echo "==> Preparing package root..."
mkdir -p "${PKG_ROOT}/usr/local/bin"
mkdir -p "${PKG_ROOT}/etc/${CONFIG_DIR}"
cp "${BUILD_DIR}/${NAME}" "${PKG_ROOT}/usr/local/bin/${NAME}"
sed "s|/var/log/agent-tools-cli.log|/var/log/${NAME}.log|" config-example.yaml > "${PKG_ROOT}/etc/${CONFIG_DIR}/config-example.yaml"
chmod 755 "${PKG_ROOT}/usr/local/bin/${NAME}"

echo "==> Building .pkg..."
pkgbuild \
  --root "${PKG_ROOT}" \
  --identifier "${PKG_IDENTIFIER}" \
  --version "${VERSION}" \
  --install-location "/" \
  "${NAME}-${VERSION}-macos.pkg"

echo ""
echo "==> Done: ${NAME}-${VERSION}-macos.pkg"

rm -rf "${BUILD_DIR}"
