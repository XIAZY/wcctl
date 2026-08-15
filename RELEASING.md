# Release builds

Release binaries are cross-compiled for macOS on Linux with
[osxcross](https://github.com/tpoechtrager/osxcross). The toolchain revision,
SDK version, Go version, and minimum deployment target are pinned in
`Dockerfile.osxcross`.

The Docker build downloads the macOS 15.2 SDK from the versioned
[`joseluisq/macosx-sdks` release](https://github.com/joseluisq/macosx-sdks/releases/tag/15.2)
and verifies its pinned SHA-256 before using it. Review the Xcode license terms
before building.

## Test the release build locally

Install Docker, then run:

```bash
WCCTL_VERSION=v0.0.0 ./scripts/build-release.sh
```

The command runs Linux containers to build both architectures and writes:

```text
dist/wcctl-darwin-amd64.tar.gz
dist/wcctl-darwin-arm64.tar.gz
dist/SHA256SUMS
```

The initial build creates the osxcross toolchain and can take several minutes.
Docker caches that layer for later builds.

## Publish a release

Create and push a version tag:

```bash
git tag v0.0.1
git push origin v0.0.1
```

The workflow passes the pushed tag to the Docker build, embeds it in both
binaries, verifies the embedded value, and then creates a GitHub Release with
both tarballs and `SHA256SUMS` attached. On macOS, the released binary reports
that tag with `wcctl version`.
