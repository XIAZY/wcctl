#!/usr/bin/env bash

set -euo pipefail

readonly repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly output_directory="${1:-${repository_root}/dist}"
readonly dockerfile="${repository_root}/Dockerfile.osxcross"
readonly build_version="${WCCTL_VERSION:-dev}"

if [[ ! "${build_version}" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]*$ ]]; then
  echo "Invalid WCCTL_VERSION: ${build_version}" >&2
  exit 2
fi

for command_name in docker strings tar; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

if command -v sha256sum >/dev/null 2>&1; then
  checksum_command=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  checksum_command=(shasum -a 256)
else
  echo "Required command not found: sha256sum or shasum" >&2
  exit 1
fi

temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/wcctl-release.XXXXXX")"
cleanup() {
  rm -rf "${temporary_directory}"
}
trap cleanup EXIT

mkdir -p "${output_directory}"

for architecture in amd64 arm64; do
  artifact_directory="${temporary_directory}/${architecture}"
  mkdir -p "${artifact_directory}"

  echo "Building darwin/${architecture} with osxcross in Docker ..."
  docker buildx build \
    --file "${dockerfile}" \
    --build-arg "TARGETARCH=${architecture}" \
    --build-arg "VERSION=${build_version}" \
    --output "type=local,dest=${artifact_directory}" \
    "${repository_root}"

  binary="${artifact_directory}/wcctl"
  if [[ ! -f "${binary}" ]]; then
    echo "Docker build did not produce ${binary}" >&2
    exit 1
  fi
  chmod 0755 "${binary}"

  if ! strings "${binary}" | grep -Fx -- "${build_version}" >/dev/null; then
    echo "Binary does not contain the expected version: ${build_version}" >&2
    exit 1
  fi

  archive="${output_directory}/wcctl-darwin-${architecture}.tar.gz"
  tar -C "${artifact_directory}" -czf "${archive}" wcctl
done

(
  cd "${output_directory}"
  "${checksum_command[@]}" \
    wcctl-darwin-amd64.tar.gz wcctl-darwin-arm64.tar.gz \
    > SHA256SUMS
)

echo "Release artifacts:"
ls -lh "${output_directory}/wcctl-darwin-amd64.tar.gz" \
  "${output_directory}/wcctl-darwin-arm64.tar.gz" \
  "${output_directory}/SHA256SUMS"
