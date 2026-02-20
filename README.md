# 🖖 Captain's Log

**Local, private speech-to-text powered by [Whisper](https://github.com/openai/whisper). No cloud. No telemetry.**

Record → Transcribe → Copy. All on your hardware.

---

## Install (3 steps)

### Step 1: Start a Whisper backend

Captain's Log needs a [faster-whisper](https://github.com/SYSTRAN/faster-whisper) server running locally. The easiest way is [whisper-fastapi](https://github.com/heimoshuiyu/whisper-fastapi):

```bash
# Option A: Docker (recommended)
docker run -d -p 5000:5000 --gpus all ghcr.io/heimoshuiyu/whisper-fastapi:latest

# Option B: pip
pip install faster-whisper fastapi uvicorn
uvicorn whisper_fastapi:app --host 0.0.0.0 --port 5000
```

> **No GPU?** It works on CPU too — just slower. Use `--model small` for CPU.
>
> **Full setup guide**: See [whisper-fastapi README](https://github.com/heimoshuiyu/whisper-fastapi#readme) for all options, then come back here.

Verify it's running:

```bash
curl http://127.0.0.1:5000/v1/models
```

### Step 2: Build Captain's Log

```bash
git clone https://github.com/ryan-winkler/captainslog-whisper.git
cd captainslog-whisper
go build -o captainslog ./cmd/captainslog
```

> **Need Go?** Install from [go.dev/dl](https://go.dev/dl/) — one download, no dependencies.

### Step 3: Run it

```bash
./captainslog
```

Open **http://localhost:8090** — that's it. Hit the mic button and talk.

### Optional: Install permanently

```bash
# Copy binary to PATH
cp captainslog ~/.local/bin/

# Install CLI tool
cp captainslog-cli ~/.local/bin/

# Run as systemd user service (starts on boot)
cp examples/captainslog.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now captainslog
```

### Optional: Docker

```bash
docker build -t captainslog .
docker run -p 8090:8090 \
  -e CAPTAINSLOG_WHISPER_URL=http://host.docker.internal:5000 \
  captainslog
```

---

## CLI

Install: `cp captainslog-cli ~/.local/bin/`

```bash
# Transcribe a file
captainslog-cli transcribe meeting.mp3

# Transcribe and save to vault
captainslog-cli transcribe call.wav | captainslog-cli save

# Save text to vault
captainslog-cli save "Quick note from standup"
echo "piped text" | captainslog-cli save

# Check what's connected
captainslog-cli health
captainslog-cli models

# Change settings
captainslog-cli settings language fr
captainslog-cli settings auto_save true

# Get current stardate
captainslog-cli stardate
```

### CLI environment

| Variable | Default | What it does |
|---|---|---|
| `CAPTAINSLOG_URL` | `http://127.0.0.1:8090` | Where Captain's Log is running |
| `CAPTAINSLOG_TOKEN` | *(empty)* | Auth token if required |
| `CAPTAINSLOG_LANGUAGE` | *(from settings)* | Override language |

---

## AI agent integration

Captain's Log works with [OpenClaw](https://github.com/openclaw/openclaw), [ZeroClaw](https://github.com/openagen/zeroclaw), and any AI agent that can run shell commands or make HTTP requests.

### [OpenClaw](https://docs.openclaw.ai/install)

Captain's Log ships with an [OpenClaw skill](https://docs.openclaw.ai/skills). Install it:

```bash
# Option A: Symlink the included skill to your OpenClaw skills directory
ln -s ~/code/captainslog-whisper/skills/captainslog ~/.openclaw/skills/captainslog

# Option B: Copy it
cp -r ~/code/captainslog-whisper/skills/captainslog ~/.openclaw/skills/
```

Then use it in OpenClaw: `/captainslog transcribe meeting.mp3`

The skill gives the agent access to all CLI commands and the HTTP API. See [OpenClaw skills docs](https://docs.openclaw.ai/skills) for more.

### [ZeroClaw](https://zeroclaw.org/)

Add Captain's Log to your `SOUL.md` or workspace config so ZeroClaw knows how to use it:

```markdown
## Captain's Log (Speech-to-Text)
- CLI: `captainslog-cli transcribe <file>`, `captainslog-cli save "text"`
- API: POST http://127.0.0.1:8090/v1/audio/transcriptions (multipart, OpenAI-compatible)
- Health: GET http://127.0.0.1:8090/healthz
- Models: GET http://127.0.0.1:8090/api/models
```

ZeroClaw can call `captainslog-cli` directly via `shell_exec`, or use `http_request` to hit the API. See [ZeroClaw docs](https://zeroclaw.org/) for setup.

### Any agent (generic)

If your agent supports shell commands:

```bash
captainslog-cli transcribe recording.mp3   # → text to stdout
captainslog-cli save "text to save"        # → saves to vault
captainslog-cli health                     # → JSON status
```

If your agent supports HTTP:

```bash
# Transcribe (OpenAI-compatible)
curl -X POST http://127.0.0.1:8090/v1/audio/transcriptions \
  -F file=@recording.mp3 -F response_format=text

# Save to vault
curl -X POST http://127.0.0.1:8090/api/vault/save \
  -H "Content-Type: application/json" \
  -d '{"text":"Meeting notes...","language":"en"}'
```

---

## How it works

```
Browser / CLI / API
        ↓
Captain's Log (Go, port 8090)
        ↓
faster-whisper (port 5000)
        ↓
   Transcription
        ↓
Clipboard · Vault · AI forwarding
```

Captain's Log is a **~10MB static Go binary** with zero external dependencies. It proxies audio to [faster-whisper](https://github.com/SYSTRAN/faster-whisper) via [whisper-fastapi](https://github.com/heimoshuiyu/whisper-fastapi) and gives you a browser UI, CLI, and [OpenAI-compatible API](https://platform.openai.com/docs/api-reference/audio).

## Features

| Feature | Details |
|---|---|
| 🎙️ **Recording** | One-click browser recording with live waveform |
| 📁 **File upload** | Drag-and-drop any audio or video |
| 🖥️ **CLI** | `captainslog-cli transcribe file.mp3` — pipe-friendly |
| 🔌 **API** | OpenAI-compatible `/v1/audio/transcriptions` |
| 💾 **Vault save** | Auto-save to [Obsidian](https://obsidian.md/) daily markdown files |
| 📋 **Clipboard** | Auto-copy + transcription history (50 entries) |
| 🤖 **AI forwarding** | Send to [Ollama](https://ollama.com/), [Gemini](https://gemini.google.com/), [Claude](https://claude.ai/), [ChatGPT](https://chatgpt.com/) |
| 👯 **Diarization** | Speaker identification with color labels ([whisperX](https://github.com/m-bain/whisperX)) |
| ⏱️ **Timestamps** | Word-level clickable timestamps |
| 🔍 **Model discovery** | Auto-detects models from whisper-fastapi and Ollama |
| ⚙️ **Preferences** | All settings in UI, persisted to `settings.json` |
| 🔒 **Security** | Auth token, auto-TLS, security headers |
| 🌟 **Stardates** | Optional TNG-era display (toggle on/off) |

### Keyboard shortcuts

`Space` record · `Ctrl+C` copy · `Ctrl+S` save · `,` preferences · `Escape` close

---

## Settings

Settings are saved to `~/.config/captainslog/settings.json` automatically. They survive restarts — no env vars needed for day-to-day use.

**Priority**: env var > settings.json > defaults

### All settings

| Setting | Default | What it does |
|---|---|---|
| `language` | `en` | Transcription language ([ISO 639-1](https://en.wikipedia.org/wiki/List_of_ISO_639-1_codes)) |
| `model` | `large-v3` | [Whisper model](https://github.com/openai/whisper#available-models-and-languages) |
| `auto_copy` | `true` | Copy to clipboard after transcription |
| `auto_save` | `false` | Auto-save to vault |
| `date_format` | `2006-01-02` | Date format for vault files (ISO/EU/US) |
| `file_title` | `Dictation` | Heading in vault markdown files |
| `show_stardates` | `true` | Show stardates or normal time in header |
| `diarize` | `false` | Speaker diarization |
| `vad_filter` | `false` | [Voice activity detection](https://en.wikipedia.org/wiki/Voice_activity_detection) filter |
| `prompt` | *(empty)* | Initial transcription prompt |
| `vault_dir` | *(empty)* | Vault save directory |

### Portable settings with [rclone](https://rclone.org/)

Sync settings across machines by pointing the config dir at a cloud-synced path:

```bash
# Move to Google Drive (or Dropbox, OneDrive, S3, etc.)
mv ~/.config/captainslog ~/GoogleDrive/Config/captainslog
ln -s ~/GoogleDrive/Config/captainslog ~/.config/captainslog
```

Or set a custom location:

```bash
export CAPTAINSLOG_CONFIG_DIR=/mnt/nas/config/captainslog
```

Works with any [rclone backend](https://rclone.org/overview/), [Syncthing](https://syncthing.net/), or shared drive.

---

## Environment variables

For server-level config (systemd, Docker). Most users won't need these — use Preferences in the UI instead.

| Variable | Default | Description |
|---|---|---|
| `CAPTAINSLOG_PORT` | `8090` | HTTP port |
| `CAPTAINSLOG_HOST` | `0.0.0.0` | Bind address |
| `CAPTAINSLOG_WHISPER_URL` | `http://127.0.0.1:5000` | [Whisper backend](https://github.com/heimoshuiyu/whisper-fastapi) |
| `CAPTAINSLOG_OLLAMA_URL` | `http://127.0.0.1:11434` | [Ollama](https://ollama.com/) |
| `CAPTAINSLOG_AUTH_TOKEN` | *(empty)* | Bearer token for auth |
| `CAPTAINSLOG_VAULT_DIR` | *(empty)* | [Obsidian](https://obsidian.md/) vault path |
| `CAPTAINSLOG_CONFIG_DIR` | `~/.config/captainslog` | Settings file location |
| `CAPTAINSLOG_ENABLE_OLLAMA` | `false` | Enable [Ollama](https://ollama.com/) |
| `CAPTAINSLOG_ENABLE_TLS` | `false` | Auto-generate TLS cert |
| `CAPTAINSLOG_TLS_HOSTNAMES` | *(empty)* | Extra TLS SANs |
| `CAPTAINSLOG_LANGUAGE` | `en` | Default language |
| `CAPTAINSLOG_MODEL` | `large-v3` | Default model |
| `CAPTAINSLOG_DATE_FORMAT` | `2006-01-02` | Vault date format ([Go layout](https://pkg.go.dev/time#pkg-constants)) |
| `CAPTAINSLOG_FILE_TITLE` | `Dictation` | Vault heading prefix |

---

## Integrations

| Integration | How |
|---|---|
| [**faster-whisper**](https://github.com/SYSTRAN/faster-whisper) | Transcription engine — 4x faster than OpenAI Whisper, via [whisper-fastapi](https://github.com/heimoshuiyu/whisper-fastapi) |
| [**Home Assistant**](https://www.home-assistant.io/) | Local voice via [Wyoming protocol](https://www.home-assistant.io/integrations/wyoming/) on `:10300` |
| [**Ollama**](https://ollama.com/) | Post-processing: punctuation, summaries, filler removal |
| [**Obsidian**](https://obsidian.md/) | Daily markdown files with [YAML frontmatter](https://help.obsidian.md/Editing+and+formatting/Properties) |
| [**`.home.arpa`**](https://www.rfc-editor.org/rfc/rfc8375) | [RFC 8375](https://www.rfc-editor.org/rfc/rfc8375) home network DNS + `CAPTAINSLOG_ENABLE_TLS=true` for mic access |

---

## API

| Endpoint | Method | Description |
|---|---|---|
| `/v1/audio/transcriptions` | `POST` | [OpenAI-compatible](https://platform.openai.com/docs/api-reference/audio/createTranscription) (multipart) |
| `/v1/audio/translations` | `POST` | Translate to English |
| `/api/settings` | `GET`/`PUT` | Persistent settings |
| `/api/vault/save` | `POST` | Save to vault (`{"text":"..."}`) |
| `/api/models` | `GET` | Available models |
| `/api/stardate` | `GET` | Current [stardate](https://en.wikipedia.org/wiki/Stardate) |
| `/healthz` | `GET` | Status check |

---

## Contributing

1. Fork ([github.com/ryan-winkler/captainslog-whisper/fork](https://github.com/ryan-winkler/captainslog-whisper/fork))
2. Branch (`git checkout -b feature/warp-core-upgrade`)
3. Commit (`git commit -am '✨ Add warp core upgrade'`)
4. Push + PR

```bash
go run ./cmd/captainslog      # Dev server
go build -o captainslog ./cmd/captainslog
go test ./...
```

**Code style**: Go stdlib only · vanilla HTML/CSS/JS · env vars + settings.json · never commit secrets

---

## Security

- All audio processed locally — never leaves your machine
- No telemetry, analytics, or tracking
- Optional auth token, auto-TLS, security headers
- [`.home.arpa`](https://www.rfc-editor.org/rfc/rfc8375) domains for secure LAN access

## License

[MIT](LICENSE)

---

*Live long and transcribe.* 🖖
