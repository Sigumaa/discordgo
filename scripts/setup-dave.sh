#!/usr/bin/env bash
#
# setup-dave.sh — Build libdave and install dependencies for discordgo DAVE support.
#
# Usage:
#   ./scripts/setup-dave.sh [OPTIONS]
#
# Options:
#   --ssl <openssl_3|openssl_1.1|boringssl>  SSL variant (default: openssl_3)
#   --libdave-dir <path>                     Path to existing libdave clone (default: auto-clone)
#   --help                                   Show this help
#
# This script:
#   1. Clones discord/libdave (if not already present)
#   2. Builds it with vcpkg dependencies
#   3. Copies headers and static libraries into dave/deps/{include,lib}
#
# After running this script, `go build ./...` will work.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPS_DIR="$PROJECT_ROOT/dave/deps"

SSL_VARIANT="openssl_3"
LIBDAVE_DIR=""

usage() {
    sed -n '3,/^$/s/^# \?//p' "$0"
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --ssl)       SSL_VARIANT="$2"; shift 2 ;;
        --libdave-dir) LIBDAVE_DIR="$2"; shift 2 ;;
        --help|-h)   usage ;;
        *)           echo "Unknown option: $1"; usage ;;
    esac
done

# --- Detect platform ---
detect_triplet() {
    local os arch
    case "$(uname -s)" in
        Linux*)  os="linux" ;;
        Darwin*) os="osx" ;;
        *)       echo "Error: unsupported OS: $(uname -s)"; exit 1 ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64) arch="x64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)             echo "Error: unsupported architecture: $(uname -m)"; exit 1 ;;
    esac

    echo "${arch}-${os}"
}

TRIPLET="$(detect_triplet)"
echo "==> Detected platform triplet: $TRIPLET"

# --- Check prerequisites ---
check_prereqs() {
    local missing=()
    command -v cmake  >/dev/null 2>&1 || missing+=(cmake)
    command -v make   >/dev/null 2>&1 || missing+=(make)
    command -v git    >/dev/null 2>&1 || missing+=(git)
    command -v pkg-config >/dev/null 2>&1 || missing+=(pkg-config)

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "Error: missing required tools: ${missing[*]}"
        echo ""
        case "$(uname -s)" in
            Linux*)
                echo "Install with:"
                echo "  sudo apt-get install ${missing[*]}    # Debian/Ubuntu"
                echo "  sudo dnf install ${missing[*]}        # Fedora/RHEL"
                ;;
            Darwin*)
                echo "Install with:"
                echo "  brew install ${missing[*]}"
                ;;
        esac
        exit 1
    fi
}

check_prereqs

# --- Clone libdave if needed ---
if [[ -z "$LIBDAVE_DIR" ]]; then
    LIBDAVE_DIR="$PROJECT_ROOT/.libdave"
    if [[ ! -d "$LIBDAVE_DIR" ]]; then
        echo "==> Cloning discord/libdave into $LIBDAVE_DIR"
        git clone --depth 1 https://github.com/discord/libdave.git "$LIBDAVE_DIR"
    else
        echo "==> Using existing libdave at $LIBDAVE_DIR"
    fi
fi

# --- Initialize submodules ---
echo "==> Initializing submodules"
git -C "$LIBDAVE_DIR" submodule update --init --recursive

# --- Bootstrap vcpkg ---
VCPKG_DIR="$LIBDAVE_DIR/cpp/vcpkg"
if [[ ! -x "$VCPKG_DIR/vcpkg" ]]; then
    echo "==> Bootstrapping vcpkg"
    "$VCPKG_DIR/bootstrap-vcpkg.sh" -disableMetrics
fi

# --- Build libdave ---
LIBDAVE_CPP="$LIBDAVE_DIR/cpp"
echo "==> Building libdave (SSL=$SSL_VARIANT, BUILD_TYPE=Release, PERSISTENT_KEYS=ON)"
make -C "$LIBDAVE_CPP" all SSL="$SSL_VARIANT" BUILD_TYPE=Release PERSISTENT_KEYS=ON

echo "==> Installing libdave"
make -C "$LIBDAVE_CPP" install SSL="$SSL_VARIANT" BUILD_TYPE=Release PERSISTENT_KEYS=ON

# --- Copy artifacts to dave/deps ---
echo "==> Installing to $DEPS_DIR"
rm -rf "$DEPS_DIR"
mkdir -p "$DEPS_DIR/include" "$DEPS_DIR/lib"

# Headers
cp -r "$LIBDAVE_CPP/build/install/include/dave" "$DEPS_DIR/include/"

# libdave static library
cp "$LIBDAVE_CPP/build/install/lib/libdave.a" "$DEPS_DIR/lib/"

# vcpkg dependency libraries
VCPKG_LIB="$LIBDAVE_CPP/build/vcpkg_installed/$TRIPLET/lib"
if [[ ! -d "$VCPKG_LIB" ]]; then
    echo "Error: vcpkg lib directory not found at $VCPKG_LIB"
    echo "Available triplets:"
    ls "$LIBDAVE_CPP/build/vcpkg_installed/" 2>/dev/null || echo "  (none)"
    exit 1
fi

for lib in mlspp mls_vectors mls_ds bytes tls_syntax hpke ssl crypto; do
    src="$VCPKG_LIB/lib${lib}.a"
    if [[ -f "$src" ]]; then
        cp "$src" "$DEPS_DIR/lib/"
    else
        echo "Warning: $src not found, skipping"
    fi
done

echo ""
echo "==> Done! libdave dependencies installed to dave/deps/"
echo "    You can now build with: go build ./..."
