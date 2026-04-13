#!/bin/bash
set -e

NAME="agent-tools-cli"
VERSION="${VERSION:-0.0.1}"
RELEASE="1"
BUILD_DIR=$(mktemp -d)
SOURCES_DIR="$HOME/rpmbuild/SOURCES"
SPEC_FILE="$BUILD_DIR/${NAME}.spec"

echo "==> Building ${NAME} binary..."
cd "$(dirname "$0")"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${NAME}" .

echo "==> Preparing RPM source tarball..."
which rpmbuild >/dev/null 2>&1 || dnf -y install rpm-build

mkdir -p "${SOURCES_DIR}"
mkdir -p "${BUILD_DIR}/${NAME}-${VERSION}"
cp "${NAME}" "${BUILD_DIR}/${NAME}-${VERSION}/"
cp config-example.yaml "${BUILD_DIR}/${NAME}-${VERSION}/"

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
agent-tools-cli lets you query and manage resources across all clusters.

%prep
%setup -q

%install
install -m 755 -d %{buildroot}%{_bindir}
install -Dm755 ${NAME} %{buildroot}%{_bindir}/${NAME}
install -Dm644 config-example.yaml %{buildroot}%{_sysconfdir}/agent-tools/config-example.yaml

# NOTE: User config (~/.agent-tools/config.yaml) is intentionally NOT managed by this
# RPM package. It contains sensitive data (url, token, role_id) and must never
# be modified or deleted during install, upgrade, or uninstall.

%post
if [ \$1 -eq 1 ]; then
    echo ""
    echo "========================================================"
    echo "  agent-tools-cli installed successfully!"
    echo ""
    echo "  NEXT STEP: copy the config template and fill it in:"
    echo ""
    echo "    cp /etc/agent-tools/config-example.yaml ~/.agent-tools/config.yaml"
    echo "    vi ~/.agent-tools/config.yaml"
    echo ""
    echo "  Set your url, token, and role_id in the config file."
    echo "========================================================"
    echo ""
fi

%preun
# On uninstall (not upgrade): do NOT touch ~/.agent-tools/config.yaml
# \$1 == 0 means final removal; \$1 == 1 means upgrade — either way, config is preserved
:

%postun
# Config file ~/.agent-tools/config.yaml is user-owned and not managed by RPM.
# It will NOT be removed on uninstall or upgrade.
:

%files
%{_bindir}/${NAME}
%{_sysconfdir}/agent-tools/config-example.yaml

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
