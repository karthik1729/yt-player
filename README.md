# yt-player

YouTube Music player you control over MCP. Music plays in a background daemon, so it keeps going after the MCP client disconnects.

## Requirements

- Go 1.26+
- [mpv](https://mpv.io) and [yt-dlp](https://github.com/yt-dlp/yt-dlp) on `PATH` (`brew install mpv yt-dlp`)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/karthik1729/yt-player/main/install.sh | sh
```

This puts the latest macOS or Linux binary in `~/.local/bin` (override with `INSTALL_DIR=/usr/local/bin`). Binaries are also on [Releases](https://github.com/karthik1729/yt-player/releases), or build from source:

```bash
go install github.com/karthik1729/yt-player@latest
```

Register it with Claude Code:

```bash
claude mcp add yt-player -- yt-player
```

Or in any MCP client config:

```json
{ "mcpServers": { "yt-player": { "command": "yt-player" } } }
```

## How it works

- `yt-player` is a stdio MCP bridge. It connects to the daemon's socket and starts the daemon if it isn't running.
- `yt-player daemon` owns one mpv process and the queue. It listens on `~/Library/Caches/yt-player/daemon.sock` (`~/.cache/yt-player` on Linux), readable only by you.
- Stop everything with the `stop` tool, or `pkill -f "yt-player daemon"`.
- Scripts and status bars can call any tool on the running daemon: `yt-player call status`, `yt-player call volume '{"level":40}'`.

## Tools

| Tool | Purpose |
| --- | --- |
| `search` | Search songs, videos, albums, artists, playlists |
| `browse` | Home, charts, moods & genres, new releases |
| `open` | Album, artist, playlist or category page |
| `play` | Replace the queue with a song, radio, playlist, album or artist |
| `queue_add` | Append to the queue |
| `watch` | Open a video in its own mpv window (pauses the music) |
| `queue` | List the queue |
| `jump` | Play queue index |
| `next` / `previous` | Skip tracks |
| `pause` / `resume` | Pause and resume |
| `seek` | Seek absolute or relative |
| `volume` | Volume 0-100 |
| `shuffle` | Shuffle upcoming tracks |
| `repeat` | off, all, one |
| `status` | Current track, position, volume |
| `stop` | Stop and clear the queue |
