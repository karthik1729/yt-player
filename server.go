package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Daemon struct {
	player *Player

	mu       sync.Mutex
	queue    []Item
	pos      int // index of current track; -1 or len(queue) when idle
	repeat   string
	failures int // consecutive tracks that failed to load
}

func newDaemon(mpvSock string) *Daemon {
	d := &Daemon{pos: -1, repeat: "off"}
	d.player = &Player{sock: mpvSock, onEnd: d.onEnd}
	return d
}

func (d *Daemon) playAt(i int) (string, error) {
	if i < 0 || i >= len(d.queue) {
		d.pos = len(d.queue)
		if d.player.Running() {
			d.player.Command("stop")
		}
		return "End of queue.", nil
	}
	t := d.queue[i]
	if _, err := d.player.Command("loadfile", "https://music.youtube.com/watch?v="+t.VideoID, "replace"); err != nil {
		return "", err
	}
	d.pos = i
	d.player.Command("set_property", "pause", false)
	return fmt.Sprintf("Playing %d. %s", i, t.label()), nil
}

func (d *Daemon) step(n int) int {
	i := d.pos + n
	if d.repeat == "all" && len(d.queue) > 0 {
		i = (i%len(d.queue) + len(d.queue)) % len(d.queue)
	}
	return max(i, 0)
}

func (d *Daemon) onEnd(reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if reason == "error" {
		d.failures++
		if d.failures >= 3 { // stream extraction is broken; don't burn through the queue
			d.failures = 0
			d.pos = len(d.queue)
			return
		}
	} else {
		d.failures = 0
	}
	i := d.pos
	if d.repeat != "one" || reason == "error" {
		i = d.step(1)
	}
	d.playAt(i)
}

type Source struct {
	Query      string `json:"query,omitempty" jsonschema:"search text; uses the top song"`
	VideoID    string `json:"videoId,omitempty" jsonschema:"YouTube video id"`
	PlaylistID string `json:"playlistId,omitempty" jsonschema:"playlist id; uses all its tracks"`
	BrowseID   string `json:"browseId,omitempty" jsonschema:"album or artist browseId; uses its tracks"`
	Radio      bool   `json:"radio,omitempty" jsonschema:"with query or videoId: add related tracks after it"`
}

var (
	videoRe = regexp.MustCompile(`^[\w-]{11}$`)
	idRe    = regexp.MustCompile(`^[\w-]+$`)
)

func resolve(ctx context.Context, s Source) ([]Item, error) {
	videoID := s.VideoID
	if s.Query != "" {
		secs, err := search(ctx, s.Query, "songs")
		if err != nil {
			return nil, err
		}
		if t := tracks(secs); len(t) > 0 {
			videoID = t[0].VideoID
		} else {
			return nil, errors.New("no results")
		}
	}
	var list []Item
	var err error
	switch {
	case videoID != "":
		if !videoRe.MatchString(videoID) {
			return nil, errors.New("invalid videoId")
		}
		list, err = watchList(ctx, videoID, "RDAMVM"+videoID)
		if !s.Radio && len(list) > 0 {
			list = list[:1]
		}
	case s.PlaylistID != "":
		if !idRe.MatchString(s.PlaylistID) {
			return nil, errors.New("invalid playlistId")
		}
		if strings.HasPrefix(s.PlaylistID, "RD") {
			list, err = watchList(ctx, "", s.PlaylistID)
		} else {
			var secs []Section
			secs, err = browse(ctx, "VL"+strings.TrimPrefix(s.PlaylistID, "VL"), "")
			list = tracks(secs)
		}
	case s.BrowseID != "":
		if !idRe.MatchString(s.BrowseID) {
			return nil, errors.New("invalid browseId")
		}
		var secs []Section
		secs, err = browse(ctx, s.BrowseID, "")
		list = tracks(secs)
	default:
		return nil, errors.New("provide query, videoId, playlistId or browseId")
	}
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("nothing playable found")
	}
	return list, nil
}

type (
	None        struct{}
	SearchInput struct {
		Query string `json:"query"`
		Type  string `json:"type,omitempty" jsonschema:"all, songs, videos, albums, artists or playlists (default songs)"`
	}
	BrowseInput struct {
		Section string `json:"section,omitempty" jsonschema:"home, charts, moods_and_genres or new_releases"`
	}
	OpenInput struct {
		BrowseID string `json:"browseId" jsonschema:"browseId of an album, artist, playlist (VL...) or category"`
		Params   string `json:"params,omitempty" jsonschema:"params from a category item"`
	}
	IndexInput struct {
		Index int `json:"index" jsonschema:"0-based queue index"`
	}
	SeekInput struct {
		Seconds  float64 `json:"seconds"`
		Relative bool    `json:"relative,omitempty" jsonschema:"seek relative to current position"`
	}
	VolumeInput struct {
		Level int `json:"level" jsonschema:"0-100"`
	}
	WatchInput struct {
		Query   string `json:"query,omitempty" jsonschema:"search text; opens the top video"`
		VideoID string `json:"videoId,omitempty" jsonschema:"YouTube video id"`
	}
	MuteInput struct {
		On bool `json:"on" jsonschema:"true to mute, false to unmute"`
	}
	RepeatInput struct {
		Mode string `json:"mode" jsonschema:"off, all or one"`
	}
)

var browseIDs = map[string]string{
	"": "FEmusic_home", "home": "FEmusic_home", "charts": "FEmusic_charts",
	"moods_and_genres": "FEmusic_moods_and_genres", "new_releases": "FEmusic_new_releases_albums",
}

func tool[In any](s *mcp.Server, name, desc string, fn func(context.Context, In) (any, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			v, err := fn(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			text, ok := v.(string)
			if !ok {
				b, _ := json.MarshalIndent(v, "", "  ")
				text = string(b)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
}

func (d *Daemon) server() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "yt-player", Version: "1.0.0"}, nil)

	tool(s, "search", "Search YouTube Music", func(ctx context.Context, in SearchInput) (any, error) {
		return search(ctx, in.Query, in.Type)
	})
	tool(s, "browse", "Browse YouTube Music home, charts, moods & genres, or new releases", func(ctx context.Context, in BrowseInput) (any, error) {
		id, ok := browseIDs[in.Section]
		if !ok {
			return nil, errors.New("unknown section")
		}
		return browse(ctx, id, "")
	})
	tool(s, "open", "Open an album, artist, playlist or category page", func(ctx context.Context, in OpenInput) (any, error) {
		if !idRe.MatchString(in.BrowseID) {
			return nil, errors.New("invalid browseId")
		}
		return browse(ctx, in.BrowseID, in.Params)
	})

	tool(s, "play", "Replace the queue with a song, radio, playlist, album or artist and start playing", func(ctx context.Context, in Source) (any, error) {
		list, err := resolve(ctx, in)
		if err != nil {
			return nil, err
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		d.queue, d.pos, d.failures = list, -1, 0
		return d.playAt(0)
	})
	tool(s, "queue_add", "Append a song, radio, playlist, album or artist to the queue", func(ctx context.Context, in Source) (any, error) {
		list, err := resolve(ctx, in)
		if err != nil {
			return nil, err
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		idle := d.pos < 0 || d.pos >= len(d.queue)
		start := len(d.queue)
		d.queue = append(d.queue, list...)
		if idle {
			return d.playAt(start)
		}
		return fmt.Sprintf("Queued %d track(s).", len(list)), nil
	})
	tool(s, "queue", "List the queue", func(ctx context.Context, _ None) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.queue) == 0 {
			return "Queue is empty.", nil
		}
		var b strings.Builder
		for i, t := range d.queue {
			mark := "  "
			if i == d.pos {
				mark = "▶ "
			}
			fmt.Fprintf(&b, "%s%d. %s\n", mark, i, t.label())
		}
		return b.String(), nil
	})
	tool(s, "jump", "Play the queue item at index", func(ctx context.Context, in IndexInput) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.playAt(in.Index)
	})
	tool(s, "next", "Next track", func(ctx context.Context, _ None) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.playAt(d.step(1))
	})
	tool(s, "previous", "Previous track", func(ctx context.Context, _ None) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.playAt(d.step(-1))
	})
	tool(s, "pause", "Pause", func(ctx context.Context, _ None) (any, error) {
		_, err := d.player.Command("set_property", "pause", true)
		return "Paused.", err
	})
	tool(s, "resume", "Resume", func(ctx context.Context, _ None) (any, error) {
		_, err := d.player.Command("set_property", "pause", false)
		return "Resumed.", err
	})
	tool(s, "stop", "Stop playback and clear the queue", func(ctx context.Context, _ None) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.queue, d.pos = nil, -1
		if d.player.Running() {
			d.player.Command("stop")
		}
		return "Stopped.", nil
	})
	tool(s, "seek", "Seek to a position in seconds", func(ctx context.Context, in SeekInput) (any, error) {
		mode := "absolute"
		if in.Relative {
			mode = "relative"
		}
		_, err := d.player.Command("seek", in.Seconds, mode)
		return "Seeked.", err
	})
	tool(s, "volume", "Set volume 0-100", func(ctx context.Context, in VolumeInput) (any, error) {
		_, err := d.player.Command("set_property", "volume", min(max(in.Level, 0), 100))
		return fmt.Sprintf("Volume %d.", in.Level), err
	})
	tool(s, "watch", "Open a YouTube video in its own mpv window (pauses the music)", func(ctx context.Context, in WatchInput) (any, error) {
		id, title := in.VideoID, in.VideoID
		if in.Query != "" {
			secs, err := search(ctx, in.Query, "videos")
			if err != nil {
				return nil, err
			}
			t := tracks(secs)
			if len(t) == 0 {
				return nil, errors.New("no videos found")
			}
			id, title = t[0].VideoID, t[0].Title
		}
		if !videoRe.MatchString(id) {
			return nil, errors.New("provide query or a valid videoId")
		}
		cmd := exec.Command("mpv", "--force-window=immediate", "https://www.youtube.com/watch?v="+id)
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start mpv: %w", err)
		}
		go cmd.Wait()
		if d.player.Running() {
			d.player.Command("set_property", "pause", true)
		}
		return "Opened video: " + title, nil
	})
	tool(s, "mute", "Mute or unmute", func(ctx context.Context, in MuteInput) (any, error) {
		_, err := d.player.Command("set_property", "mute", in.On)
		if in.On {
			return "Muted.", err
		}
		return "Unmuted.", err
	})
	tool(s, "shuffle", "Shuffle the upcoming tracks in the queue", func(ctx context.Context, _ None) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		rest := d.queue[min(max(d.pos+1, 0), len(d.queue)):]
		rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
		return fmt.Sprintf("Shuffled %d upcoming track(s).", len(rest)), nil
	})
	tool(s, "repeat", "Set repeat mode", func(ctx context.Context, in RepeatInput) (any, error) {
		if in.Mode != "off" && in.Mode != "all" && in.Mode != "one" {
			return nil, errors.New("mode must be off, all or one")
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		d.repeat = in.Mode
		return "Repeat " + in.Mode + ".", nil
	})
	tool(s, "status", "Current track, position, volume and repeat mode", func(ctx context.Context, _ None) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		st := map[string]any{"repeat": d.repeat, "queueLength": len(d.queue)}
		if d.pos >= 0 && d.pos < len(d.queue) {
			st["index"], st["current"] = d.pos, d.queue[d.pos]
			for _, p := range []string{"time-pos", "duration", "pause", "volume", "mute"} {
				if v, err := d.player.Command("get_property", p); err == nil {
					st[p] = v
				}
			}
		}
		return st, nil
	})
	return s
}
