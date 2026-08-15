# Release builds

Release binaries are cross-compiled for macOS on Linux with
[osxcross](https://github.com/tpoechtrager/osxcross). The toolchain revision,
SDK version, Go version, and minimum deployment target are pinned in
`Dockerfile.osxcross`.

The macOS SDK is required to build osxcross, but it is not part of this
repository or its Docker image source. Obtain and use the SDK only as permitted
by Apple's terms.

## Test the release build locally

Install Xcode Command Line Tools containing the macOS 15.2 SDK and Docker
Desktop on a Mac, then run:

```bash
./scripts/package-macos-sdk.sh
./scripts/build-release.sh
```

The first command packages the installed macOS 15.2 SDK into the ignored
`.osxcross` directory. The second command runs Linux containers to build both
architectures and writes:

```text
dist/wcctl-darwin-amd64.tar.gz
dist/wcctl-darwin-arm64.tar.gz
dist/SHA256SUMS
```

The initial build creates the osxcross toolchain and can take several minutes.
Docker caches that layer for later builds.

## Configure GitHub Actions

The release workflow needs two repository secrets:

- `MACOS_SDK_URL`: a private HTTPS URL from which the runner can download the
  `MacOSX15.2.sdk.tar.xz` archive.
- `MACOS_SDK_SHA256`: the SHA-256 printed by `package-macos-sdk.sh`.

The URL must be accessible non-interactively from a GitHub-hosted Ubuntu runner.
Do not publish the SDK as a release asset or commit it to the repository.

If the SDK is installed somewhere other than the standard Command Line Tools
directory, set its path while packaging:

```bash
MACOS_SDK_PATH=/path/to/MacOSX15.2.sdk ./scripts/package-macos-sdk.sh
```

## Publish a release

After the secrets are configured, create and push a version tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow builds both architectures inside Docker on Ubuntu, verifies the
artifacts, and creates a GitHub Release for the tag with both tarballs and
`SHA256SUMS` attached.
