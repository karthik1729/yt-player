package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Item is anything YouTube Music lists: a song, album, artist, playlist or category.
type Item struct {
	Type       string `json:"type"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"`
	VideoID    string `json:"videoId,omitempty"`
	BrowseID   string `json:"browseId,omitempty"`
	PlaylistID string `json:"playlistId,omitempty"`
	Params     string `json:"params,omitempty"`
	Duration   int    `json:"durationSeconds,omitempty"`
}

func (it Item) label() string {
	s := it.Title
	if it.Subtitle != "" {
		s += " — " + it.Subtitle
	}
	return s + " [" + it.VideoID + "]"
}

type Section struct {
	Title string `json:"title,omitempty"`
	Items []Item `json:"items"`
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func innertube(ctx context.Context, endpoint string, body map[string]any) (any, error) {
	body["context"] = map[string]any{"client": map[string]any{
		"clientName": "WEB_REMIX", "clientVersion": "1.20250101.01.00", "hl": "en", "gl": "US",
	}}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://music.youtube.com/youtubei/v1/"+endpoint+"?prettyPrint=false", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music %s: %s", endpoint, resp.Status)
	}
	var out any
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func get(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func str(v any, path ...string) string {
	s, _ := get(v, path...).(string)
	return s
}

func runs(v any) string {
	if s := str(v, "simpleText"); s != "" {
		return s
	}
	rs, _ := get(v, "runs").([]any)
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(str(r, "text"))
	}
	return b.String()
}

// walk visits every object in document order; fn returns true to skip its children.
func walk(v any, fn func(key string, m map[string]any) bool) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if m, ok := t[k].(map[string]any); ok && fn(k, m) {
				continue
			}
			walk(t[k], fn)
		}
	case []any:
		for _, e := range t {
			walk(e, fn)
		}
	}
}

var durationRe = regexp.MustCompile(`^\d+(:\d{2})+$`)

func applyEndpoint(it *Item, nav any) {
	if id := str(nav, "watchEndpoint", "videoId"); id != "" {
		it.VideoID = id
	}
	if id := str(nav, "watchPlaylistEndpoint", "playlistId"); id != "" {
		it.PlaylistID = id
	}
	id := str(nav, "browseEndpoint", "browseId")
	if id == "" {
		return
	}
	it.BrowseID = id
	it.Params = str(nav, "browseEndpoint", "params")
	switch str(nav, "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType") {
	case "MUSIC_PAGE_TYPE_ALBUM":
		it.Type = "album"
	case "MUSIC_PAGE_TYPE_ARTIST", "MUSIC_PAGE_TYPE_USER_CHANNEL":
		it.Type = "artist"
	case "MUSIC_PAGE_TYPE_PLAYLIST":
		it.Type = "playlist"
		it.PlaylistID = strings.TrimPrefix(id, "VL")
	}
}

func parseItem(key string, m map[string]any) (Item, bool) {
	var it Item
	var parts []string
	switch key {
	case "musicResponsiveListItemRenderer":
		cols, _ := m["flexColumns"].([]any)
		fixed, _ := m["fixedColumns"].([]any)
		for i, c := range append(cols, fixed...) {
			col := get(c, "musicResponsiveListItemFlexColumnRenderer", "text")
			if col == nil {
				col = get(c, "musicResponsiveListItemFixedColumnRenderer", "text")
			}
			if i == 0 {
				it.Title = runs(col)
				if rs, ok := get(col, "runs").([]any); ok && len(rs) > 0 {
					applyEndpoint(&it, get(rs[0], "navigationEndpoint"))
				}
			} else {
				parts = append(parts, runs(col))
			}
		}
		applyEndpoint(&it, m["navigationEndpoint"])
		if id := str(m, "playlistItemData", "videoId"); id != "" {
			it.VideoID = id
		}
	case "musicTwoRowItemRenderer":
		it.Title = runs(m["title"])
		parts = append(parts, runs(m["subtitle"]))
		applyEndpoint(&it, m["navigationEndpoint"])
	case "playlistPanelVideoRenderer":
		it.Title = runs(m["title"])
		parts = append(parts, runs(m["longBylineText"]), runs(m["lengthText"]))
		it.VideoID = str(m, "videoId")
	case "musicNavigationButtonRenderer":
		it.Title = runs(m["buttonText"])
		it.Type = "category"
		applyEndpoint(&it, m["clickCommand"])
	default:
		return it, false
	}

	var sub []string
	for _, p := range parts {
		for _, s := range strings.Split(p, " • ") {
			switch {
			case s == "":
			case durationRe.MatchString(s):
				it.Duration = 0
				for _, n := range strings.Split(s, ":") {
					v, _ := strconv.Atoi(n)
					it.Duration = it.Duration*60 + v
				}
			default:
				sub = append(sub, s)
			}
		}
	}
	it.Subtitle = strings.Join(sub, " • ")

	if it.Type == "" {
		switch {
		case it.VideoID != "":
			it.Type = "song"
		case it.PlaylistID != "":
			it.Type = "playlist"
		case it.BrowseID != "":
			it.Type = "page"
		}
	}
	return it, it.Title != "" && it.Type != ""
}

func items(v any) []Item {
	var out []Item
	walk(v, func(k string, m map[string]any) bool {
		if k == "playlistPanelVideoWrapperRenderer" { // skip the video/song counterpart duplicate
			if p, ok := get(m, "primaryRenderer", "playlistPanelVideoRenderer").(map[string]any); ok {
				if it, ok := parseItem("playlistPanelVideoRenderer", p); ok {
					out = append(out, it)
				}
			}
			return true
		}
		it, ok := parseItem(k, m)
		if ok {
			out = append(out, it)
		}
		return ok
	})
	return out
}

var shelfKeys = map[string]bool{
	"musicShelfRenderer": true, "musicCarouselShelfRenderer": true,
	"gridRenderer": true, "musicPlaylistShelfRenderer": true,
}

func sections(v any) []Section {
	var out []Section
	walk(v, func(k string, m map[string]any) bool {
		if !shelfKeys[k] {
			return false
		}
		title := runs(m["title"])
		if title == "" {
			title = runs(get(m, "header", "musicCarouselShelfBasicHeaderRenderer", "title"))
		}
		if title == "" {
			title = runs(get(m, "header", "gridHeaderRenderer", "title"))
		}
		if its := items(m); len(its) > 0 {
			out = append(out, Section{title, its})
		}
		return true
	})
	if len(out) == 0 {
		if its := items(v); len(its) > 0 {
			out = []Section{{Items: its}}
		}
	}
	return out
}

func tracks(secs []Section) []Item {
	var out []Item
	for _, s := range secs {
		for _, it := range s.Items {
			if it.VideoID != "" {
				out = append(out, it)
			}
		}
	}
	return out
}

var searchParams = map[string]string{
	"songs":     "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D",
	"videos":    "EgWKAQIQAWoKEAkQChAFEAMQBA%3D%3D",
	"albums":    "EgWKAQIYAWoKEAkQAxAEEAkQBQ%3D%3D",
	"artists":   "EgWKAQIgAWoKEAkQChAFEAMQBA%3D%3D",
	"playlists": "EgeKAQQoAEABagoQCRAKEAMQBBAF",
}

func search(ctx context.Context, query, kind string) ([]Section, error) {
	body := map[string]any{"query": query}
	if p := searchParams[kind]; p != "" {
		body["params"] = p
	}
	r, err := innertube(ctx, "search", body)
	if err != nil {
		return nil, err
	}
	return sections(r), nil
}

// ponytail: first page only (~100 tracks for playlists); follow continuations if longer lists matter
func browse(ctx context.Context, browseID, params string) ([]Section, error) {
	body := map[string]any{"browseId": browseID}
	if params != "" {
		body["params"] = params
	}
	r, err := innertube(ctx, "browse", body)
	if err != nil {
		return nil, err
	}
	return sections(r), nil
}

// watchList returns the "up next" list: a radio for a video, or the tracks of a mix/playlist.
func watchList(ctx context.Context, videoID, playlistID string) ([]Item, error) {
	body := map[string]any{"isAudioOnly": true}
	if videoID != "" {
		body["videoId"] = videoID
	}
	if playlistID != "" {
		body["playlistId"] = playlistID
	}
	r, err := innertube(ctx, "next", body)
	if err != nil {
		return nil, err
	}
	return items(r), nil
}
