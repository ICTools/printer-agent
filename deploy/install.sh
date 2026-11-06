#!/usr/bin/env bash
# Install (or repair, or migrate) the print agent on a till PC, without Go:
#
#   curl -fsSL https://raw.githubusercontent.com/ICTools/printer-agent/main/deploy/install.sh | bash
#
# Options (after `bash -s --` when piped):
#   --version vX.Y.Z   install that release instead of the latest
#   --from-source      build from this checkout with Go (development)
#
# It downloads the binary of the GitHub release for this CPU, checks its sha256
# against checksums.txt, then runs `printer setup`, which installs the binary
# to ~/.local/bin/printer, the systemd user service, the config
# ~/.config/print-agent/env (never overwritten) and enables start at boot.
#
# Private repository: export GITHUB_TOKEN=... (or log in with `gh auth login`).
set -euo pipefail

# Everything runs from main(), called on the last line: if `curl | bash` is cut
# off mid-download, bash never runs a partial script.
main() {
REPO="${PRINTER_REPO:-ICTools/printer-agent}"
VERSION="latest"
FROM_SOURCE=0

usage() {
    cat <<'USAGE'
Usage: install.sh [--version vX.Y.Z] [--from-source]
  piped:  curl -fsSL https://raw.githubusercontent.com/ICTools/printer-agent/main/deploy/install.sh | bash -s -- --version v1.0.0
  --version vX.Y.Z   install that release instead of the latest
  --from-source      build from this checkout with Go (development)
Private repository: export GITHUB_TOKEN=... or `gh auth login` first.
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="${2:?--version needs a tag, e.g. v1.0.0}"; shift 2 ;;
        --from-source) FROM_SOURCE=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown option: $1" >&2; exit 2 ;;
    esac
done

if [ "$(id -u)" -eq 0 ]; then
    echo "Run this as the till user, not root/sudo: the service is a systemd *user* service." >&2
    exit 1
fi
if [ "$(uname -s)" != "Linux" ]; then
    echo "Linux only." >&2
    exit 1
fi

# Not /tmp: it may be mounted noexec, and the downloaded binary is executed.
mkdir -p "${XDG_CACHE_HOME:-$HOME/.cache}"
TMP="$(mktemp -d "${XDG_CACHE_HOME:-$HOME/.cache}/printer-install.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

if [ "$FROM_SOURCE" -eq 1 ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    REPO_DIR="$(dirname "$SCRIPT_DIR")"
    if [ ! -f "$REPO_DIR/go.mod" ]; then
        echo "--from-source must be run from a checkout: ./deploy/install.sh --from-source" >&2
        exit 1
    fi
    command -v go >/dev/null || { echo "Go is required for --from-source." >&2; exit 1; }
    BUILD_VERSION="$(git -C "$REPO_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
    echo "Building printer ${BUILD_VERSION} from ${REPO_DIR}..."
    (cd "$REPO_DIR" && CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w -X main.version=${BUILD_VERSION}" -o "$TMP/printer" ./cmd/printer)
    "$TMP/printer" setup </dev/null
    exit 0
fi

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
ASSET="printer_linux_${ARCH}"

if [ "$VERSION" = "latest" ]; then
    BASE="https://github.com/${REPO}/releases/latest/download"
else
    BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi

download_public() {
    curl -fsSL --retry 3 -o "$TMP/$ASSET" "$BASE/$ASSET" &&
        curl -fsSL --retry 3 -o "$TMP/checksums.txt" "$BASE/checksums.txt"
}

download_gh() {
    command -v gh >/dev/null || return 1
    local tag=()
    if [ "$VERSION" != "latest" ]; then tag=("$VERSION"); fi
    # gh uses GH_TOKEN / GITHUB_TOKEN when set, else its own login.
    gh release download "${tag[@]}" -R "$REPO" -p "$ASSET" -p checksums.txt -D "$TMP" --clobber
}

echo "Downloading ${ASSET} (${VERSION}) from ${REPO}..."
if ! download_public 2>/dev/null; then
    echo "Public download failed, trying the gh CLI (private repository?)..."
    if ! download_gh; then
        cat >&2 <<EOF
Download failed. Check that a release exists:
  https://github.com/${REPO}/releases
If the repository is private: install gh and run \`gh auth login\`,
or export GITHUB_TOKEN=<token with read access to ${REPO}>, then retry.
EOF
        exit 1
    fi
fi

echo "Verifying sha256..."
if ! (cd "$TMP" && grep -E "^[0-9a-f]{64} [ *]${ASSET}\$" checksums.txt | sha256sum -c --strict -); then
    echo "Checksum verification FAILED: nothing was installed." >&2
    exit 1
fi
chmod 0755 "$TMP/$ASSET"

"$TMP/$ASSET" setup </dev/null

cat <<EOF

Done. Useful commands (open a new terminal if 'printer' is not found yet):
  printer status         state of the service, config and printers
  printer config edit    fill in the API credentials (restarts the service)
  printer log            follow the logs
  printer update         install the latest release
EOF
}

main "$@"
