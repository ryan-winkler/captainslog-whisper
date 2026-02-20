---
name: captainslog
description: Transcribe audio files and manage voice notes via Captain's Log
homepage: https://github.com/ryan-winkler/captainslog-whisper
user-invocable: true
---

# Captain's Log — Speech-to-Text Skill

You have access to a local speech-to-text service via `captainslog-cli` (CLI) or via `http_request` to `http://127.0.0.1:8090`.

## CLI Usage (preferred)

```bash
# Transcribe audio/video files
captainslog-cli transcribe <file>

# Save text to Obsidian vault
captainslog-cli save "text here"
echo "text" | captainslog-cli save

# Check available models
captainslog-cli models

# Check backend health
captainslog-cli health

# Get/set settings
captainslog-cli settings
captainslog-cli settings language fr
```

## HTTP API

```
POST http://127.0.0.1:8090/v1/audio/transcriptions  (multipart, OpenAI-compatible)
POST http://127.0.0.1:8090/api/vault/save            (JSON: {"text":"...","language":"en"})
GET  http://127.0.0.1:8090/api/settings
PUT  http://127.0.0.1:8090/api/settings              (JSON partial update)
GET  http://127.0.0.1:8090/api/models
GET  http://127.0.0.1:8090/api/stardate
GET  http://127.0.0.1:8090/healthz
```

## Notes

- Captain's Log runs locally at http://127.0.0.1:8090
- All audio is processed locally via faster-whisper — never sent externally
- Settings persist to ~/.config/captainslog/settings.json
- Vault files saved to the configured vault directory as daily markdown
