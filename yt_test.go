package main

import (
	"encoding/json"
	"testing"
)

func TestParse(t *testing.T) {
	const doc = `{"contents":[
	 {"musicShelfRenderer":{"title":{"runs":[{"text":"Songs"}]},"contents":[
	  {"musicResponsiveListItemRenderer":{
	   "flexColumns":[
	    {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"Instant Crush","navigationEndpoint":{"watchEndpoint":{"videoId":"khnokW3Mw24"}}}]}}},
	    {"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"Daft Punk"},{"text":" • "},{"text":"5:38"}]}}}],
	   "playlistItemData":{"videoId":"khnokW3Mw24"}}},
	  {"musicResponsiveListItemRenderer":{
	   "flexColumns":[{"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"RAM"}]}}}],
	   "navigationEndpoint":{"browseEndpoint":{"browseId":"MPREb_x","browseEndpointContextSupportedConfigs":{"browseEndpointContextMusicConfig":{"pageType":"MUSIC_PAGE_TYPE_ALBUM"}}}}}}]}},
	 {"playlistPanelVideoWrapperRenderer":{
	  "primaryRenderer":{"playlistPanelVideoRenderer":{"title":{"runs":[{"text":"Get Lucky"}]},"videoId":"5NV6Rdv1a3I","lengthText":{"runs":[{"text":"4:09"}]}}},
	  "counterpart":[{"counterpartRenderer":{"playlistPanelVideoRenderer":{"title":{"runs":[{"text":"dup"}]},"videoId":"xxxxxxxxxxx"}}}]}}]}`
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatal(err)
	}

	secs := sections(v)
	if len(secs) != 1 || secs[0].Title != "Songs" || len(secs[0].Items) != 2 {
		t.Fatalf("sections: %+v", secs)
	}
	song, album := secs[0].Items[0], secs[0].Items[1]
	if song.Type != "song" || song.VideoID != "khnokW3Mw24" || song.Duration != 338 || song.Subtitle != "Daft Punk" {
		t.Errorf("song: %+v", song)
	}
	if album.Type != "album" || album.BrowseID != "MPREb_x" {
		t.Errorf("album: %+v", album)
	}

	all := items(v)
	if len(all) != 3 || all[2].VideoID != "5NV6Rdv1a3I" || all[2].Duration != 249 {
		t.Errorf("items (wrapper counterpart must be skipped): %+v", all)
	}
}
