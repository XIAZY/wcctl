# wcctl

`wcctl` is a macOS command-line tool for finding and verifying the final
AES-256 keys used by WeChat 4.x databases, then reading contacts, chatrooms,
sessions, and messages from those databases.

## Read this first

- Use `wcctl` only with accounts, processes, and data you are authorized to
  access and only as permitted by the [LICENSE](LICENSE).
- Acquiring keys **freezes and terminates WeChat and its child processes**.
  Save unfinished work in WeChat before continuing.
- Key acquisition requires System Integrity Protection (SIP) to be disabled.
  Disabling SIP weakens important macOS protections. Apple recommends doing so
  only temporarily and re-enabling it as soon as possible.
- Memory captures can contain messages, credentials, and other private data.
  `wcctl` makes them private and normally deletes temporary captures after
  successful extraction.
- `~/.wcctl/keys.json` contains database keys in plaintext hexadecimal.
  Protect it as sensitive data.

Contact, chatroom, session, and message queries are read-only. They do not
require root access or disabled SIP after the keys have been acquired.

## Requirements

To run a prebuilt executable:

- macOS 12 Monterey or newer
- WeChat 4.x installed and signed in
- An administrator account for the temporary capture step

To build from source, also install:

- Go 1.25 or newer
- Xcode Command Line Tools or Xcode

SQLCipher is embedded in the executable. Users do not need Homebrew,
`sqlcipher`, or OpenSSL.

## Quick start

### 1. Build

From the repository directory:

```bash
MACOSX_DEPLOYMENT_TARGET=12.0 CGO_ENABLED=1 \
  go build -trimpath -o wcctl .
```

Confirm that the CLI starts:

```bash
./wcctl
```

On first startup, read the displayed license attestation and type `yes` only
if every statement is true. Acceptance is stored in
`~/.wcctl/config.json`.

### 2. Temporarily disable SIP

Check the current status:

```bash
csrutil status
```

If SIP is enabled, restart into macOS Recovery:

- **Apple silicon:** shut down the Mac, then hold the power button until startup
  options appear. Select **Options**, then **Continue**.
- **Intel:** restart and hold **Command-R** during startup.

In Recovery, open **Utilities → Terminal** and run:

```bash
csrutil disable
```

Restart into macOS. Apple documents this process in
[Disabling and Enabling System Integrity Protection](https://developer.apple.com/documentation/security/disabling-and-enabling-system-integrity-protection).

Verify the result:

```bash
csrutil status
```

It must report:

```text
System Integrity Protection status: disabled.
```

### 3. Open WeChat

Open WeChat, sign in, and allow the account and recent conversations to finish
loading. Leave WeChat running.

### 4. Acquire database keys

Run this as your regular desktop user—**do not prefix it with `sudo`**:

```bash
./wcctl key acquire
```

The command will:

1. Verify that SIP is disabled.
2. Find the main executable at `WeChat.app/Contents/MacOS/WeChat`.
3. Discover the local WeChat account database directory.
4. Show the selected process and account.
5. Warn that WeChat will be terminated and ask for confirmation.
6. Use `/usr/bin/sudo` only for the memory-capture helper.
7. Freeze the discovered process tree and dump the main WeChat process's
   writable memory.
8. Terminate the frozen process tree without resuming it.
9. Extract candidate keys and cryptographically verify them against each
   database.
10. Merge verified keys into `~/.wcctl/keys.json`.
11. Delete the temporary capture after success.

WeChat is not reopened automatically. Restart it yourself when you are ready.

The freeze and dump are fail-closed. A failed `SIGSTOP`, memory read, segment
write, metadata write, or flush aborts the capture. Processes already stopped
are killed rather than resumed, and partial output is retained for diagnosis.

Useful overrides:

```bash
# Select a specific WeChat account or process.
./wcctl key acquire -account ACCOUNT
./wcctl key acquire -pid PID

# Keep the sensitive capture after successful extraction.
./wcctl key acquire -keep-dump

# Keep it at a specific path. Explicit output is never automatically deleted.
./wcctl key acquire -out ./capture

# Use alternate database and key-store locations.
./wcctl key acquire -data-dir /path/to/xwechat_files \
  -keys /path/to/keys.json
```

Use `-yes` only for controlled automation. It skips the destructive prompt but
does not skip the license, SIP, process, account, or privilege checks.

### 5. Select a default user when necessary

If `keys.json` contains more than one WeChat account:

```bash
./wcctl user ls
./wcctl user use ACCOUNT
./wcctl user current
```

The selected account is stored as `default_user` in `config.json`. A command's
`-user ACCOUNT` flag overrides it for that invocation without changing the
default.

### 6. Query WeChat data

List regular contacts:

```bash
./wcctl contact ls
./wcctl contact ls -json
```

This excludes chatrooms, room-only members, official or verified accounts,
deleted entries, and known built-in identities.

List chatrooms:

```bash
./wcctl chatroom ls
./wcctl chatroom ls -json
```

List recent conversation sessions:

```bash
./wcctl session ls
./wcctl session ls -limit 100 -json
```

List messages using the canonical username shown by the contact, chatroom, or
session commands:

```bash
./wcctl message ls -chat wxid_example
./wcctl message ls -chat 123456789@chatroom -limit 100 -json
```

Use `-before` with a Unix timestamp or RFC3339 time for older pages:

```bash
./wcctl message ls -chat wxid_example \
  -before 2026-08-01T00:00:00Z
```

Message listing searches every keyed `message_N.db` shard, resolves sender
IDs, decodes WCDB Zstandard-compressed text, and merges results by time. It
reports media and packed-resource metadata but does not export image, video,
voice, or emoticon payloads.

### 7. Re-enable SIP

After acquiring keys, restart into Recovery again, open Terminal, and run:

```bash
csrutil enable
```

Restart and verify:

```bash
csrutil status
```

Normal database queries continue to work with SIP enabled.

## Extract keys from an existing capture

If acquisition retained a capture, retry extraction without opening or
terminating WeChat again:

```bash
./wcctl key extract -capture /path/to/capture
```

Specify the account or database location when auto-discovery is ambiguous:

```bash
./wcctl key extract -capture /path/to/capture \
  -account ACCOUNT \
  -data-dir /path/to/xwechat_files \
  -keys /path/to/keys.json
```

`extract-key -in PATH` remains available as a deprecated compatibility alias.

## Expert: capture memory manually

The guided `key acquire` command is recommended. For a manual two-step
workflow:

```bash
sudo ./wcctl dump -pid PID -out ./dumps
./wcctl key extract -capture ./dumps
```

`dump` requires root, disabled SIP, a live PID, an empty non-symlink output
directory, and destructive confirmation. By default it captures writable
regions from the selected root process, skips the dyld shared cache, freezes
the discovered process tree, and terminates that tree afterward.

Expert flags:

```text
-full       include all readable regions
-shared     include the dyld shared cache
-meta       write region metadata without reading region contents
-chunk N    read memory in N MiB chunks (default 4)
-yes        skip destructive confirmation
```

Even `-meta` uses the same freeze-and-terminate lifecycle.

## Files and privacy

Default user files:

```text
~/.wcctl/config.json   license acceptance and default user
~/.wcctl/keys.json     verified database paths and AES keys
```

Both files are mode `0600`; `~/.wcctl` is mode `0700`.

A retained capture contains:

```text
capture/
├── audit.log
├── regions.jsonl
└── segments/
    └── <virtual-address>.bin
```

Capture directories are mode `0700`, and their files are mode `0600`. Do not
upload or share captures unless you have intentionally reviewed the privacy and
security consequences.

The key-store format groups databases by account:

```json
{
  "users": {
    "wxid_example": {
      "databases": {
        "/absolute/path/to/message_0.db": {
          "aes_key": "...",
          "updated_at": "2026-08-15T00:00:00Z"
        }
      }
    }
  }
}
```

## Troubleshooting

### `System Integrity Protection is enabled`

Run `csrutil status`. Key acquisition requires the exact disabled status. Follow
the Recovery instructions above, then restart before retrying.

### `WeChat is not running`

Open the main WeChat application, sign in, and retry. Helpers such as
`WeChatAppEx`, renderers, GPU processes, `wxutility`, and `wxplayer` are
intentionally ignored.

### `process PID is not the main WeChat process`

Omit `-pid` and let `wcctl` detect the executable, or provide the PID whose
path ends exactly with:

```text
/WeChat.app/Contents/MacOS/WeChat
```

### Multiple accounts or processes were found

Select interactively or pass `-account ACCOUNT` and `-pid PID`. Use
`wcctl user use ACCOUNT` to remember the normal query default.

### No candidate keys were found or verified

- Confirm WeChat was signed in and fully loaded before acquisition.
- Check the retained capture path printed after failure.
- Retry with `wcctl key extract -capture PATH` before capturing again.
- Verify that `-data-dir` points to the matching account's `db_storage` data.

### `sudo` was cancelled or failed

Run `key acquire` as the regular desktop user and enter an administrator
credential only when macOS prompts for the capture helper. Do not run the whole
command with `sudo`.

### A partial capture was retained

Strict acquisition stops at the first freeze, memory, or output failure. The
partial capture is retained so its `audit.log` and `regions.jsonl` can be
inspected. It may still contain sensitive memory even though extraction did not
complete.

## Release builds

Build both macOS architectures and combine them into a universal executable:

```bash
MACOSX_DEPLOYMENT_TARGET=12.0 CGO_ENABLED=1 GOARCH=arm64 \
  go build -trimpath -o wcctl-arm64 .

MACOSX_DEPLOYMENT_TARGET=12.0 CGO_ENABLED=1 GOARCH=amd64 \
  go build -trimpath -o wcctl-amd64 .

lipo -create wcctl-arm64 wcctl-amd64 \
  -output wcctl-universal
```

SQLCipher 4.16.0 is vendored and compiled with Apple's CommonCrypto provider.
The executable has no Homebrew, external SQLCipher, or OpenSSL runtime
dependency. It still links to standard macOS system frameworks. See
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES) for dependency provenance and terms.

## License

`wcctl` is distributed under the
[Data Interoperability Source License 1.0](LICENSE). Third-party components
retain their own licenses as listed in [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
