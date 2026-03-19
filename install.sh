#!/bin/sh
set -e

REPO="ntancardoso/gb"
BINARY="gb"
INSTALL_DIR=""

# Colors (disabled if not a TTY)
if [ -t 1 ]; then
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  CYAN='\033[0;36m'
  BOLD='\033[1m'
  RESET='\033[0m'
else
  RED='' GREEN='' YELLOW='' CYAN='' BOLD='' RESET=''
fi

info()    { printf "${CYAN}==>${RESET} ${BOLD}%s${RESET}\n" "$1"; }
success() { printf "${GREEN}✓${RESET} %s\n" "$1"; }
warn()    { printf "${YELLOW}warning:${RESET} %s\n" "$1"; }
fatal()   { printf "${RED}error:${RESET} %s\n" "$1" >&2; exit 1; }

TMP_DIR=""
cleanup() {
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT INT TERM

detect_os() {
  case "$(uname -s)" in
    Linux*)           echo "linux" ;;
    Darwin*)          echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "linux" ;;  # Git Bash/MSYS2: use linux binary
    *)                fatal "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *)             fatal "Unsupported architecture: $(uname -m)" ;;
  esac
}

fetch_latest_version() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' \
      | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' \
      | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
  else
    fatal "curl or wget is required"
  fi
}

get_installed_version() {
  if command -v "$BINARY" >/dev/null 2>&1; then
    ver=$("$BINARY" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    if [ -n "$ver" ]; then
      echo "v${ver}"
    fi
  fi
}

download() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    if [ -t 1 ]; then
      curl -fL --progress-bar "$url" -o "$dest"
    else
      curl -fsSL "$url" -o "$dest"
    fi
  else
    wget -q "$url" -O "$dest"
  fi
}

verify_checksum() {
  archive="$1"
  checksums_file="$2"
  archive_name="$(basename "$archive")"

  expected="$(grep " ${archive_name}$" "$checksums_file" | awk '{print $1}')"
  if [ -z "$expected" ]; then
    warn "No checksum entry for ${archive_name}, skipping verification"
    return
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  else
    warn "sha256sum/shasum not found, skipping checksum verification"
    return
  fi

  if [ "$actual" != "$expected" ]; then
    printf "${RED}error:${RESET} Checksum mismatch!\n  expected: %s\n  got:      %s\n" "$expected" "$actual" >&2
    exit 1
  fi
  success "Checksum verified"
}

select_install_dir() {
  if [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
  elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    echo "/usr/local/bin"
  else
    echo "${HOME}/.local/bin"
  fi
}

install_binary() {
  src="$1"
  dest_dir="$2"
  dest="${dest_dir}/${BINARY}"
  backup="${dest}.old"

  mkdir -p "$dest_dir"

  use_sudo=""
  [ -w "$dest_dir" ] || use_sudo="sudo"
  [ -n "$use_sudo" ] && info "Requesting sudo to write to ${dest_dir}"

  # Backup existing binary
  if [ -f "$dest" ]; then
    $use_sudo cp "$dest" "$backup"
  fi

  # Install, restore on failure
  if $use_sudo cp "$src" "$dest" && $use_sudo chmod 755 "$dest"; then
    $use_sudo rm -f "$backup"
  else
    if [ -f "$backup" ]; then
      warn "Install failed, restoring previous version..."
      $use_sudo mv "$backup" "$dest"
    fi
    fatal "Installation failed"
  fi
}

check_path() {
  dir="$1"
  case ":${PATH}:" in
    *":${dir}:"*) ;;
    *)
      warn "${dir} is not in your PATH"
      printf "  Add to your shell profile (~/.bashrc, ~/.zshrc, etc.):\n"
      printf "    ${BOLD}export PATH=\"\$PATH:${dir}\"${RESET}\n"
      ;;
  esac
}

uninstall() {
  INSTALLED_PATH="$(command -v "$BINARY" 2>/dev/null)"
  if [ -z "$INSTALLED_PATH" ]; then
    warn "gb is not installed (not found in PATH)"
    exit 0
  fi
  info "Removing ${INSTALLED_PATH}..."
  if [ -w "$(dirname "$INSTALLED_PATH")" ]; then
    rm -f "$INSTALLED_PATH"
  else
    sudo rm -f "$INSTALLED_PATH"
  fi
  success "gb uninstalled"
}

main() {
  # Parse args
  for arg in "$@"; do
    case "$arg" in
      --uninstall) uninstall; exit 0 ;;
    esac
  done

  OS="$(detect_os)"
  ARCH="$(detect_arch)"

  info "Fetching latest release..."
  VERSION="$(fetch_latest_version)"
  [ -n "$VERSION" ] || fatal "Could not determine latest version"

  # Check existing installation
  INSTALLED_VERSION="$(get_installed_version)"
  if [ -n "$INSTALLED_VERSION" ]; then
    INSTALLED_PATH="$(command -v "$BINARY")"
    if [ "$INSTALLED_VERSION" = "$VERSION" ]; then
      success "gb ${VERSION} is already installed at ${INSTALLED_PATH} — nothing to do"
      exit 0
    else
      info "Updating gb ${INSTALLED_VERSION} → ${VERSION}  (at ${INSTALLED_PATH})"
    fi
  else
    info "Installing gb ${VERSION}"
  fi

  VER="${VERSION#v}"
  ASSET="gb_${VER}_${OS}_${ARCH}.tar.gz"
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

  TMP_DIR="$(mktemp -d)"
  ARCHIVE="${TMP_DIR}/${ASSET}"
  CHECKSUMS="${TMP_DIR}/checksums.txt"

  info "Downloading ${ASSET}..."
  download "${BASE_URL}/${ASSET}" "$ARCHIVE"
  download "${BASE_URL}/checksums.txt" "$CHECKSUMS"

  info "Verifying checksum..."
  verify_checksum "$ARCHIVE" "$CHECKSUMS"

  info "Extracting..."
  tar -xzf "$ARCHIVE" -C "$TMP_DIR"

  EXTRACTED_BIN="${TMP_DIR}/${BINARY}"
  [ -f "$EXTRACTED_BIN" ] || fatal "Binary '${BINARY}' not found in archive"

  INSTALL_DIR="$(select_install_dir)"
  info "Installing to ${INSTALL_DIR}..."
  install_binary "$EXTRACTED_BIN" "$INSTALL_DIR"

  check_path "$INSTALL_DIR"

  if [ -n "$INSTALLED_VERSION" ]; then
    success "gb updated ${INSTALLED_VERSION} → ${VERSION}"
  else
    success "gb ${VERSION} installed successfully"
  fi

  printf "\n"
  if command -v "$BINARY" >/dev/null 2>&1; then
    "$BINARY" --version
  else
    warn "gb is not in PATH yet. Open a new shell or update your PATH."
  fi
}

main "$@"
