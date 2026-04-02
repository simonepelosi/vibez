#!/usr/bin/env sh
# vibez installer — https://github.com/simonepelosi/vibez
#
# Usage (review before running!):
#   curl --proto '=https' --tlsv1.2 -sSf \
#     https://raw.githubusercontent.com/simonepelosi/vibez/main/scripts/install.sh | sh
#
# Override the install directory:
#   VIBEZ_INSTALL_DIR=/usr/local/bin curl ... | sh

set -eu

REPO="simonepelosi/vibez"
BIN="vibez"
INSTALL_DIR="${VIBEZ_INSTALL_DIR:-${HOME}/.local/bin}"

# ── colours ───────────────────────────────────────────────────────────────────

setup_colors() {
    if [ -t 1 ] && command -v tput >/dev/null 2>&1; then
        BOLD="$(tput bold   2>/dev/null || printf '')"
        DIM="$(tput dim     2>/dev/null || printf '')"
        PURPLE="$(tput setaf 5 2>/dev/null || printf '')"
        GREEN="$(tput setaf 2 2>/dev/null || printf '')"
        YELLOW="$(tput setaf 3 2>/dev/null || printf '')"
        RED="$(tput setaf 1 2>/dev/null || printf '')"
        RESET="$(tput sgr0  2>/dev/null || printf '')"
    else
        BOLD="" DIM="" PURPLE="" GREEN="" YELLOW="" RED="" RESET=""
    fi
}

setup_colors

# ── helpers ───────────────────────────────────────────────────────────────────

step()    { printf '\n%s==>%s %s%s%s\n'    "${BOLD}${PURPLE}" "${RESET}" "${BOLD}" "$*" "${RESET}"; }
info()    { printf '    %s%s%s\n'          "${DIM}" "$*" "${RESET}"; }
success() { printf '    %s✓%s  %s\n'      "${GREEN}" "${RESET}" "$*"; }
warn()    { printf '    %s⚠%s  %s\n'      "${YELLOW}" "${RESET}" "$*" >&2; }
die()     { printf '\n    %s✗%s  %s\n\n'  "${RED}" "${RESET}" "$*" >&2; exit 1; }

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

# Download a URL to a file, preferring curl then wget.
download() {
    url="$1"; dst="$2"
    if command -v curl >/dev/null 2>&1; then
        curl --proto '=https' --tlsv1.2 -sSfL -o "${dst}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${dst}" "${url}"
    else
        die "Neither curl nor wget found — install one and try again."
    fi
}

# Fetch a URL to stdout (for the GitHub API, no file needed).
fetch() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl --proto '=https' --tlsv1.2 -sSf "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "${url}"
    else
        die "Neither curl nor wget found — install one and try again."
    fi
}

# ── header ────────────────────────────────────────────────────────────────────

printf '\n'
printf '%s    ♪  vibez installer%s\n' "${BOLD}${PURPLE}" "${RESET}"
printf '%s    ────────────────────────────────────────%s\n' "${DIM}" "${RESET}"
printf '\n'
info "https://github.com/${REPO}"
printf '\n'

# ── platform check ────────────────────────────────────────────────────────────

step "Checking platform…"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m)"

case "${ARCH_RAW}" in
    x86_64)          ARCH="amd64" ;;
    aarch64|arm64)   ARCH="arm64" ;;
    *)               die "Unsupported architecture: ${ARCH_RAW}" ;;
esac

[ "${OS}" = "linux" ] || die "vibez currently supports Linux only (got: ${OS})"
[ "${ARCH}" = "amd64" ] || die "vibez currently ships amd64 binaries only (got: ${ARCH_RAW})"

need_cmd tar
need_cmd grep
need_cmd sed

success "Linux ${ARCH_RAW}"

# ── resolve latest release ────────────────────────────────────────────────────

step "Fetching latest release…"

VERSION="$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"

[ -n "${VERSION}" ] || die "Could not determine latest version from GitHub API."

success "Latest: ${VERSION}"

# ── already up to date? ───────────────────────────────────────────────────────

CURRENT_VERSION=""
if [ -x "${INSTALL_DIR}/${BIN}" ]; then
    # "vibez version" prints "vibez <tag>", e.g. "vibez v0.1.0"
    CURRENT_VERSION="$("${INSTALL_DIR}/${BIN}" version 2>/dev/null | awk '{print $2}' || printf '')"
fi

if [ -n "${CURRENT_VERSION}" ]; then
    if [ "${CURRENT_VERSION}" = "${VERSION}" ]; then
        printf '\n'
        printf '%s    ✓  vibez %s is already up to date.%s\n\n' \
            "${BOLD}${GREEN}" "${VERSION}" "${RESET}"
        exit 0
    fi
    info "Installed: ${CURRENT_VERSION}  →  upgrading to ${VERSION}"
fi

# ── set up temp dir ───────────────────────────────────────────────────────────

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT INT TERM

ARCHIVE="${BIN}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

# ── download ──────────────────────────────────────────────────────────────────

step "Downloading ${BIN} ${VERSION}…"
info "${BASE_URL}/${ARCHIVE}"

download "${BASE_URL}/${ARCHIVE}"    "${TMP}/${ARCHIVE}"
download "${BASE_URL}/checksums.txt" "${TMP}/checksums.txt"

success "Download complete"

# ── verify checksum ───────────────────────────────────────────────────────────

step "Verifying checksum…"

cd "${TMP}"
if command -v sha256sum >/dev/null 2>&1; then
    grep "${ARCHIVE}" checksums.txt | sha256sum --check --status \
        || die "Checksum mismatch — the download may be corrupted. Please retry."
elif command -v shasum >/dev/null 2>&1; then
    grep "${ARCHIVE}" checksums.txt | shasum -a 256 --check --status \
        || die "Checksum mismatch — the download may be corrupted. Please retry."
else
    warn "sha256sum not found — skipping checksum verification."
fi
cd - >/dev/null

success "Checksum OK"

# ── install ───────────────────────────────────────────────────────────────────

step "Installing to ${INSTALL_DIR}…"

mkdir -p "${INSTALL_DIR}"
tar -xzf "${TMP}/${ARCHIVE}" -C "${TMP}"
cp "${TMP}/${BIN}" "${INSTALL_DIR}/${BIN}"
chmod 755 "${INSTALL_DIR}/${BIN}"

success "${INSTALL_DIR}/${BIN}"

# ── PATH check & update ───────────────────────────────────────────────────────

step "Checking PATH…"

case ":${PATH}:" in
    *":${INSTALL_DIR}:"*)
        success "${INSTALL_DIR} is already in PATH"
        ;;
    *)
        warn "${INSTALL_DIR} is not in your PATH"

        # Pick the right shell profile.
        SHELL_NAME="$(basename "${SHELL:-sh}")"
        case "${SHELL_NAME}" in
            zsh)
                PROFILE="${ZDOTDIR:-${HOME}}/.zshrc"
                EXPORT_LINE="export PATH=\"\${HOME}/.local/bin:\${PATH}\""
                ;;
            bash)
                # bash prefers .bash_profile for login shells; .bashrc for interactive
                PROFILE="${HOME}/.bashrc"
                [ -f "${HOME}/.bash_profile" ] && PROFILE="${HOME}/.bash_profile"
                EXPORT_LINE="export PATH=\"\${HOME}/.local/bin:\${PATH}\""
                ;;
            fish)
                PROFILE="${HOME}/.config/fish/config.fish"
                EXPORT_LINE="fish_add_path \${HOME}/.local/bin"
                ;;
            *)
                PROFILE="${HOME}/.profile"
                EXPORT_LINE="export PATH=\"\${HOME}/.local/bin:\${PATH}\""
                ;;
        esac

        # Append only if the line is not already present.
        if ! grep -qF "${EXPORT_LINE}" "${PROFILE}" 2>/dev/null; then
            mkdir -p "$(dirname "${PROFILE}")"
            printf '\n# Added by vibez installer\n%s\n' "${EXPORT_LINE}" >> "${PROFILE}"
            info "Added to ${PROFILE}"
        fi

        printf '\n'
        printf '%s    Restart your terminal or run:%s\n' "${YELLOW}" "${RESET}"
        printf '    %s  source %s%s\n' "${BOLD}" "${PROFILE}" "${RESET}"
        ;;
esac

# ── done ──────────────────────────────────────────────────────────────────────

printf '\n'
printf '%s    ♪  vibez %s installed successfully!%s\n' "${BOLD}${GREEN}" "${VERSION}" "${RESET}"
printf '\n'
printf '    Run %svibez auth login%s to connect your Apple Music account.\n' "${BOLD}" "${RESET}"
printf '    Then %svibez%s to start.\n' "${BOLD}" "${RESET}"
printf '\n'
