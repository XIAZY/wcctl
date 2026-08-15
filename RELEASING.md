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

The release workflow needs one repository secret containing a random encryption
key. Create it once with GitHub CLI:

```bash
openssl rand -hex 32 | gh secret set MACOS_SDK_KEY --repo XIAZY/wcctl
```

On each release, a GitHub-hosted macOS runner packages the macOS 15.2 SDK from
Xcode 16.2 and encrypts it with this key. The encrypted archive is transferred
as a short-lived workflow artifact to the Ubuntu job, which decrypts it and
builds with osxcross in Docker. The workflow deletes the transfer artifact when
the release job finishes. The SDK is never committed or attached to a release.

If the SDK is installed somewhere other than the standard Command Line Tools
directory, set its path while packaging:

```bash
MACOS_SDK_PATH=/path/to/MacOSX15.2.sdk ./scripts/package-macos-sdk.sh
```

## Publish a release

After the secrets are configured, create and push a version tag:

```bash
git tag v0.0.1
git push origin v0.0.1
```

The workflow obtains the SDK on macOS, then builds both architectures inside
Docker on Ubuntu and creates a GitHub Release for the tag with both tarballs and
`SHA256SUMS` attached.
