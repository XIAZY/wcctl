#!/bin/sh

set -eu

repository="XIAZY/wcctl"
version="latest"
install_directory="${WCCTL_INSTALL_DIR:-/usr/local/bin}"

usage() {
  cat <<'EOF'
Install the latest wcctl binary release for macOS.

Usage: install.sh [--dir DIRECTORY] [--version VERSION]

Options:
  --dir DIRECTORY   Install directory (default: /usr/local/bin)
  --version VERSION Install a specific release, such as v0.0.1
  -h, --help        Show this help

Environment:
  WCCTL_INSTALL_DIR  Alternative install directory
  WCCTL_ARCH         Override architecture: amd64 or arm64
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir)
      [ "$#" -ge 2 ] || { echo "--dir requires a directory" >&2; exit 2; }
      install_directory="$2"
      shift 2
      ;;
    --version)
      [ "$#" -ge 2 ] || { echo "--version requires a release tag" >&2; exit 2; }
      version="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ -z "${install_directory}" ]; then
  echo "Install directory cannot be empty." >&2
  exit 2
fi

if [ "$(uname -s)" != "Darwin" ]; then
  echo "wcctl binary releases currently support macOS only." >&2
  exit 1
fi

for command_name in awk curl grep install mkdir mktemp shasum tar uname; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

architecture="${WCCTL_ARCH:-}"
if [ -z "${architecture}" ]; then
  architecture="$(uname -m)"
  if [ "${architecture}" = "x86_64" ] \
      && [ "$(sysctl -in hw.optional.arm64 2>/dev/null || true)" = "1" ]; then
    architecture="arm64"
  fi

  case "${architecture}" in
    arm64|aarch64) architecture="arm64" ;;
    x86_64|amd64) architecture="amd64" ;;
    *)
      echo "Unsupported Mac architecture: ${architecture}" >&2
      exit 1
      ;;
  esac
fi

case "${architecture}" in
  amd64|arm64) ;;
  *)
    echo "Unsupported WCCTL_ARCH: ${architecture}" >&2
    exit 1
    ;;
esac

case "${version}" in
  latest)
    release_base="https://github.com/${repository}/releases/latest/download"
    ;;
  v*)
    release_base="https://github.com/${repository}/releases/download/${version}"
    ;;
  *)
    version="v${version}"
    release_base="https://github.com/${repository}/releases/download/${version}"
    ;;
esac

archive_name="wcctl-darwin-${architecture}.tar.gz"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/wcctl-install.XXXXXX")"

cleanup() {
  rm -rf "${temporary_directory}"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

download() {
  curl --fail --location --proto '=https' --tlsv1.2 --retry 3 \
    --silent --show-error --output "$2" "$1"
}

echo "Downloading wcctl ${version} for macOS/${architecture} ..."
download "${release_base}/${archive_name}" "${temporary_directory}/${archive_name}"
download "${release_base}/SHA256SUMS" "${temporary_directory}/SHA256SUMS"

expected_checksum="$(
  awk -v archive="${archive_name}" '$2 == archive { print $1 }' \
    "${temporary_directory}/SHA256SUMS"
)"
if ! printf '%s\n' "${expected_checksum}" | grep -Eq '^[0-9a-fA-F]{64}$'; then
  echo "No valid checksum found for ${archive_name}." >&2
  exit 1
fi

actual_checksum="$(shasum -a 256 "${temporary_directory}/${archive_name}" | awk '{ print $1 }')"
if [ "${actual_checksum}" != "${expected_checksum}" ]; then
  echo "Checksum verification failed for ${archive_name}." >&2
  exit 1
fi

mkdir "${temporary_directory}/extracted"
tar -xzf "${temporary_directory}/${archive_name}" \
  -C "${temporary_directory}/extracted"
binary="${temporary_directory}/extracted/wcctl"
if [ ! -f "${binary}" ]; then
  echo "Release archive does not contain wcctl." >&2
  exit 1
fi

if [ ! -d "${install_directory}" ]; then
  if ! mkdir -p "${install_directory}" 2>/dev/null; then
    if command -v sudo >/dev/null 2>&1; then
      echo "Creating ${install_directory} with administrator access ..."
      sudo mkdir -p "${install_directory}"
    else
      echo "Cannot create ${install_directory}. Choose a writable directory with --dir." >&2
      exit 1
    fi
  fi
fi

destination="${install_directory}/wcctl"
if [ -w "${install_directory}" ]; then
  install -m 0755 "${binary}" "${destination}"
elif command -v sudo >/dev/null 2>&1; then
  echo "Installing to ${install_directory} with administrator access ..."
  sudo install -m 0755 "${binary}" "${destination}"
else
  echo "Cannot write to ${install_directory}. Choose a writable directory with --dir." >&2
  exit 1
fi

echo "Installed wcctl to ${destination}"
