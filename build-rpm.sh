#!/bin/bash
set -e

NAME="agent-tools-cli"
VERSION="${VERSION:-0.0.1}"
RELEASE="1"

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
SOURCES_DIR="$HOME/rpmbuild/SOURCES"
SPEC_FILE="$BUILD_DIR/${NAME}.spec"
CONFIG_DIR="${NAME}"
LD_FLAGS="-s -w -X agent-tools/config.AppName=${NAME}"

echo "==> Building ${NAME} binary..."
cd "$(dirname "$0")"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="${LD_FLAGS}" -o "${NAME}" .

echo "==> Preparing RPM source tarball..."
which rpmbuild >/dev/null 2>&1 || dnf -y install rpm-build

mkdir -p "${SOURCES_DIR}"
mkdir -p "${BUILD_DIR}/${NAME}-${VERSION}"
cp "${NAME}" "${BUILD_DIR}/${NAME}-${VERSION}/"
sed "s|/var/log/agent-tools-cli.log|/var/log/${NAME}.log|" config-example.yaml > "${BUILD_DIR}/${NAME}-${VERSION}/config-example.yaml"

tar -zcvf "${SOURCES_DIR}/${NAME}-${VERSION}.tgz" -C "${BUILD_DIR}" "${NAME}-${VERSION}"

echo "==> Writing spec file..."
cat > "${SPEC_FILE}" << EOF
%define debug_package %{nil}

Name:       ${NAME}
Version:    ${VERSION}
Release:    ${RELEASE}
Summary:    agent-tools CLI — centralized platform management

URL:        https://github.com/your-org/agent-tools
Source:     ${NAME}-${VERSION}.tgz
License:    Proprietary

%description
${NAME} lets you query and manage resources across all clusters.

%prep
%setup -q

%install
install -m 755 -d %{buildroot}%{_bindir}
install -Dm755 ${NAME} %{buildroot}%{_bindir}/${NAME}
install -Dm644 config-example.yaml %{buildroot}%{_sysconfdir}/${CONFIG_DIR}/config-example.yaml

# NOTE: User config (~/.${NAME}/config.yaml) is intentionally NOT managed by this
# RPM package. It contains sensitive data (url, token, role_id) and must never
# be modified or deleted during install, upgrade, or uninstall.

%post
if [ \$1 -eq 1 ]; then
    echo ""
    echo "========================================================"
    echo "  ${NAME} installed successfully!"
    echo ""
    echo "  NEXT STEP: copy the config template and fill it in:"
    echo ""
    echo "    cp /etc/${CONFIG_DIR}/config-example.yaml ~/.${NAME}/config.yaml"
    echo "    vi ~/.${NAME}/config.yaml"
    echo ""
    echo "  Set your url, token, and role_id in the config file."
    echo "========================================================"
    echo ""
fi

%preun
# On uninstall (not upgrade): do NOT touch ~/.${NAME}/config.yaml
# \$1 == 0 means final removal; \$1 == 1 means upgrade — either way, config is preserved
:

%postun
# Config file ~/.${NAME}/config.yaml is user-owned and not managed by RPM.
# It will NOT be removed on uninstall or upgrade.
:

%files
%{_bindir}/${NAME}
%{_sysconfdir}/${CONFIG_DIR}/config-example.yaml

%changelog
* $(date "+%a %b %d %Y") ${BUILDER_NAME:-Builder} <${BUILDER_EMAIL:-builder@localhost}> - ${VERSION}-${RELEASE}
- Initial package
EOF

echo "==> Running rpmbuild..."
rpmbuild -bb "${SPEC_FILE}"

RPM_PATH=$(find "$HOME/rpmbuild/RPMS" -name "${NAME}-${VERSION}-*.rpm" | head -1)
echo ""
echo "==> Done: ${RPM_PATH}"

# Cleanup
rm -rf "${BUILD_DIR}"
rm -f "${NAME}"
