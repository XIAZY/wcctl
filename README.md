# wcctl

Use your local WeChat data with AI agents, scripts, search tools, and other
software.

`wcctl` gives you a simple command-line interface for reading contacts,
chatrooms, recent conversations, and messages from WeChat 4.x on macOS. Results
can be printed as a table for people or as JSON for other programs.

```bash
# See your recent conversations.
wcctl sessions

# Get message history as structured data.
wcctl messages -chat wxid_example -limit 100 -json
```

Your data stays on your Mac. Once setup is complete, all contact, chatroom,
session, and message commands are read-only and can run while WeChat is open.

## What can I do with it?

- Give a local AI agent relevant WeChat context for a task.
- Search, summarize, or analyze your own conversations.
- Export structured data to Python, `jq`, spreadsheets, or indexing tools.
- Build personal automations without uploading your WeChat database to a new
  service.
- Work with more than one WeChat account from the same installation.

For example, an agent can first list your recent sessions, choose the relevant
contact or group, and then request a limited window of messages. Because every
command supports JSON, the agent does not need to understand WeChat's database
format.

## Quick start

### 1. Install `wcctl`

You need:

- macOS 12 or newer
- WeChat 4.x

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/XIAZY/wcctl/main/install.sh | sh
```

The installer detects Apple Silicon or Intel automatically, verifies the
downloaded release checksum, and installs `wcctl` to `/usr/local/bin`. It
may ask for administrator access to write there. To install somewhere else:

```bash
curl -fsSL https://raw.githubusercontent.com/XIAZY/wcctl/main/install.sh \
  | sh -s -- --dir "$HOME/.local/bin"
```

When using a custom directory, make sure it is included in your `PATH`.

The first time you run `wcctl`, it will show the license conditions and ask
you to confirm that they apply to your use.

### 2. Set up database access

WeChat encrypts its local databases. `wcctl` needs to acquire and verify
their keys before it can read them.

This setup requires System Integrity Protection (SIP) to be disabled
temporarily. Follow
[Apple's SIP instructions](https://developer.apple.com/documentation/security/disabling-and-enabling-system-integrity-protection),
then confirm after restarting:

```bash
csrutil status
```

Open WeChat, sign in, and wait for your conversations to load. Then run the
following command from your normal macOS account:

```bash
wcctl key acquire
```

Do not add `sudo`. `wcctl` will request administrator permission for the
part that needs it.

The command guides you through account selection and tells you exactly what it
is about to do. WeChat will be closed during acquisition, so save anything
unfinished first. When acquisition succeeds, the verified keys are saved in
`~/.wcctl/keys.json` and the temporary capture is deleted.

Restart WeChat when you are ready. Re-enable SIP from macOS Recovery with
`csrutil enable`, restart the Mac, and confirm with `csrutil status`. Normal
`wcctl` queries continue to work with SIP enabled.

### 3. Explore your data

List people in your contacts:

```bash
wcctl contacts
```

List group chats:

```bash
wcctl chatrooms
```

See conversations ordered by recent activity:

```bash
wcctl sessions
```

Copy a username from one of those commands and use it to read messages:

```bash
wcctl messages -chat wxid_example
wcctl messages -chat 123456789@chatroom -limit 100
```

That is everything needed for normal use.

## Use it with an AI agent or another tool

Add `-json` to any listing command:

```bash
wcctl contacts -json
wcctl chatrooms -json
wcctl sessions -limit 100 -json
wcctl messages -chat wxid_example -limit 200 -json
```

Any local tool that can run a command and parse JSON can use `wcctl`. A
typical workflow is:

1. Run `sessions -json` to discover recent conversations.
2. Select a session by its `username`.
3. Run `messages -chat USERNAME -json` to retrieve the relevant history.
4. Pass only that result to the agent or analysis step that needs it.

It also works in ordinary shell pipelines:

```bash
wcctl sessions -limit 5 -json | jq -r '.[].username'
wcctl messages -chat wxid_example -json > messages.json
```

`wcctl` does not upload this data. The tool you connect it to decides what
happens to the JSON afterward.

## Commands

### Contacts

```bash
wcctl contacts [-user USER] [-json]
```

Lists regular contacts and their available profile metadata. Chatrooms,
official accounts, deleted contacts, and WeChat's built-in identities are not
included.

### Chatrooms

```bash
wcctl chatrooms [-user USER] [-json]
```

Lists group chats with available details such as their names, owners, member
counts, and announcements.

### Sessions

```bash
wcctl sessions [-limit N] [-user USER] [-json]
```

Lists recent conversations, including their usernames, display names, unread
state, last activity, and summaries when available. The default limit is 50.

### Messages

```bash
wcctl messages -chat USERNAME \
  [-limit N] [-before TIME] [-user USER] [-json]
```

Lists messages with a contact or chatroom. `wcctl` automatically searches
all of the local message databases and combines the results in time order. The
default limit is 50.

To retrieve older messages, pass the time of the oldest result back through
`-before`. It accepts a Unix timestamp or RFC3339 time:

```bash
wcctl messages -chat wxid_example -limit 100 \
  -before 2026-08-01T00:00:00Z -json
```

Text messages are decoded when possible. Image, video, voice, emoticon, and
other attachment metadata may be shown, but exporting the media files
themselves is not yet supported.

## Multiple accounts

If keys have been acquired for more than one WeChat account, list them and
choose a default:

```bash
wcctl user ls
wcctl user use ACCOUNT
wcctl user current
```

Use `-user ACCOUNT` when you want to switch for just one command:

```bash
wcctl messages -user ACCOUNT -chat wxid_example -json
```

With only one account, no selection is necessary.

## Key setup options

Most people only need:

```bash
wcctl key acquire
```

If auto-detection finds multiple accounts or WeChat processes, choose from the
prompt. You can also specify them directly:

```bash
wcctl key acquire -account ACCOUNT
wcctl key acquire -pid PID
```

If acquisition fails after creating a capture, retry key extraction without
closing WeChat again:

```bash
wcctl key extract -capture /path/to/capture
```

Advanced options are available for custom database locations, key-store paths,
capture locations, and automated confirmation:

```bash
wcctl key acquire -data-dir /path/to/xwechat_files
wcctl key acquire -keys /path/to/keys.json
wcctl key acquire -out ./capture
wcctl key acquire -keep-dump
wcctl key acquire -yes
```

Run `wcctl key acquire -h` or `wcctl key extract -h` for the full
option list.

## Privacy and safety

- Use `wcctl` only with accounts and data you are authorized to access and
  only as permitted by the [license](LICENSE).
- Contact, chatroom, session, and message queries do not modify WeChat's
  database records. Multiple readers are supported.
- `~/.wcctl/keys.json` contains sensitive database keys. Do not share it.
- A retained memory capture may contain messages, credentials, and other
  private data. Delete it when it is no longer needed.
- JSON output can contain private contact and message data. Be deliberate about
  which agents, services, or files receive it.

## Troubleshooting

### `System Integrity Protection is enabled`

SIP only needs to be disabled for `key acquire`. Follow Apple's Recovery
instructions, restart, and check `csrutil status` before trying again.

### WeChat is not found

Open the main WeChat application, sign in, and retry. `wcctl` intentionally
ignores helper and renderer processes.

### More than one account or process is found

Choose from the prompt, or pass `-account ACCOUNT` or `-pid PID`.

### No database key could be verified

Make sure WeChat was signed in and fully loaded, and that the selected local
account matches it. If a capture was retained, retry with
`wcctl key extract -capture PATH` before acquiring again.

### Acquisition stopped and retained a partial capture

`wcctl` stops immediately if it cannot safely pause WeChat or complete the
capture. The retained capture can help diagnose or retry the operation, but it
should be treated as sensitive data.

## License

`wcctl` is distributed under the
[Data Interoperability Source License 1.0](LICENSE). The license includes
purpose, lawful-access, and territory conditions. Read it before using or
distributing the software.

Third-party component information is available in
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
