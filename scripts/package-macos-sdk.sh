#!/usr/bin/env bash

set -euo pipefail

readonly expected_version="15.2"
readonly default_sdk_path="/Library/Developer/CommandLineTools/SDKs/MacOSX${expected_version}.sdk"
output_directory="${1:-.osxcross}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This script packages the SDK from an installed copy of Xcode on macOS." >&2
  exit 1
fi

for command_name in cp tar xz shasum; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

sdk_path="${MACOS_SDK_PATH:-${default_sdk_path}}"
if [[ ! -d "${sdk_path}" ]]; then
  echo "macOS ${expected_version} SDK not found at ${sdk_path}." >&2
  echo "Set MACOS_SDK_PATH to the directory containing that SDK." >&2
  exit 1
fi

sdk_version="$(/usr/libexec/PlistBuddy -c 'Print :Version' "${sdk_path}/SDKSettings.plist")"
if [[ "${sdk_version}" != "${expected_version}" ]]; then
  echo "Expected the macOS ${expected_version} SDK, found ${sdk_version}." >&2
  exit 1
fi

sdk_path="$(cd "${sdk_path}" && pwd -P)"
archive_name="MacOSX${sdk_version}.sdk.tar.xz"
mkdir -p "${output_directory}"
output_directory="$(cd "${output_directory}" && pwd -P)"
archive_path="${output_directory}/${archive_name}"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/wcctl-sdk.XXXXXX")"
temporary_archive="${archive_path}.tmp"

cleanup() {
  rm -rf "${temporary_directory}"
  rm -f "${temporary_archive}"
}
trap cleanup EXIT

echo "Packaging macOS ${sdk_version} SDK from ${sdk_path} ..."
cp -R "${sdk_path}" "${temporary_directory}/MacOSX${sdk_version}.sdk"
tar -C "${temporary_directory}" -cf - "MacOSX${sdk_version}.sdk" \
  | xz -T0 -6 -c > "${temporary_archive}"
mv "${temporary_archive}" "${archive_path}"

echo "Created ${archive_path}"
shasum -a 256 "${archive_path}"
