# wcctl

`wcctl` is a macOS command-line tool for acquiring process memory and
identifying the final AES-256 keys used by WeChat 4.x encrypted databases.

> `key acquire` and `dump` freeze and terminate the selected process tree.
> Read their help and use them only on processes and data you are authorized
> to inspect.

## Build

```bash
CGO_ENABLED=1 go build -o wcctl .
```

The build requires macOS, Go, and the Xcode Command Line Tools. SQLCipher
4.16.0 is vendored and compiled into the executable using Apple's CommonCrypto
provider. Users of a prebuilt executable therefore do not need Homebrew, a
`sqlcipher` executable, OpenSSL, Go, or build tools. The executable still links
to standard macOS system frameworks.

Architecture-specific release builds can be produced on macOS with:

```bash
CGO_ENABLED=1 GOARCH=arm64 go build -trimpath -o wcctl-arm64 .
CGO_ENABLED=1 GOARCH=amd64 go build -trimpath -o wcctl-amd64 .
lipo -create wcctl-arm64 wcctl-amd64 -output wcctl-universal
```

See `THIRD_PARTY_NOTICES` for dependency provenance and terms.

## Commands

On first startup, `wcctl` displays the license attestation and proceeds only
after the user confirms all required statements. Acceptance is recorded in
`~/.wcctl/config.json`; the directory is mode `0700` and the file is mode
`0600`. The config stores only the acceptance flag and confirmation timestamp,
not the user's location or data details.

When `keys.json` contains multiple users, set or inspect the persistent default
with:

```bash
./wcctl user ls
./wcctl user current
./wcctl user use USER
./wcctl user clear
```

The selected name is stored as `default_user` in `config.json`. Feature-level
`-user USER` flags override it for one invocation without changing the config.
Selection precedence is: explicit `-user`, configured default, then the sole
user in `keys.json`. A missing configured user produces an error rather than
silently switching identities.

Acquire and verify WeChat database keys with the guided workflow:

```bash
./wcctl key acquire
```

Run this command as the regular desktop user, not through `sudo`. It verifies
that SIP is disabled, finds the main running WeChat process, discovers the
account database directory, explains that WeChat will be terminated, and asks
for confirmation. Only the capture helper is elevated through `/usr/bin/sudo`.
After successful extraction, its private temporary memory capture is deleted.
Use `-keep-dump` or provide `-out DIR` to retain it; captures are retained after
failures so extraction can be retried.

For a manual two-step workflow, capture a specific process and then extract
from the resulting directory:

```bash
sudo ./wcctl dump -pid <PID> -out ./dumps
./wcctl key extract -capture ./dumps
```

The low-level `dump` command requires root, disabled SIP, a live PID, and an
explicit destructive confirmation. `extract-key` remains as a deprecated alias
for `key extract`.

List contacts using the mappings in `~/.wcctl/keys.json`:

```bash
./wcctl contact ls
```

The command lists active regular contacts, excluding chatrooms, room-only
members, official/verified accounts, deleted entries, and known built-in WeChat
identities. The default table includes common identity and relationship
metadata. Use `-json` for every non-binary column discovered in the contact
table, or `-user USER` when the key store contains more than one WeChat user.

List chatrooms with their owners, member counts, and announcement metadata:

```bash
./wcctl chatroom ls
```

As with contacts, use `-json` for the full non-binary room metadata and
`-user USER` when the key store contains multiple users.

List the latest messages for a contact or chatroom by its canonical username:

```bash
./wcctl message ls -chat wxid_example
./wcctl message ls -chat 123456789@chatroom -limit 100 -json
```

The command searches every keyed `message_N.db` shard, resolves sender IDs in
each shard, decodes WCDB Zstandard-compressed content, merges the results
chronologically, and returns the newest 50 messages by default. Use `-before`
with a Unix timestamp or RFC3339 time for older pages. Business-message, media,
resource, and FTS databases are not treated as primary message-history shards.

List recent conversation sessions:

```bash
./wcctl session ls
./wcctl session ls -limit 100 -json
```

Sessions are ordered by their last-message timestamp. The default table shows
the resolved contact or room name, canonical username, unread count, hidden
state, last sender, and summary. JSON includes drafts, unread bookkeeping,
message-type fields, timestamps, and no-contact fallback titles.

By default, `key acquire` and `key extract` discover account folders under:

```text
~/Library/Containers/com.tencent.xinWeChat/Data/Documents/xwechat_files
```

When more than one account contains a `db_storage` directory, the command
prompts for the account to test. Override discovery with either an account
folder, its `db_storage` folder, or a folder containing copied `.db` files:

```bash
./wcctl key extract -capture ./dumps -data-dir /path/to/folder
```

Each extracted 48-byte record is interpreted as a 32-byte final AES key plus a
16-byte database-salt hint. Candidates are verified against the HMAC-SHA512 tag
in the first 4096-byte page of each WeChat v4 database; the full database does
not need to be decrypted during key discovery.

Verified mappings are merged into:

```text
~/.wcctl/keys.json
```

Entries are grouped by WeChat user, with that user's database paths nested
under `databases`:

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

The directory is mode `0700` and the file is mode `0600`. The AES keys are
stored as plaintext hexadecimal inside that file, so protect it like any other
secret-bearing credential store.
