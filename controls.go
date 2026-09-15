package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// link wraps label in an OSC 8 hyperlink; the terminal decides what a click on ytp:// does.
func link(action, label string) string {
	return "\x1b]8;;ytp://" + action + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

// controls redraws a one-line player every second until killed.
func controls(sock string) {
	fmt.Print("\x1b[?25l") // hide cursor
	for {
		line := "\x1b[2mplayer not running\x1b[0m"
		if out, err := call(sock, "status", nil); err == nil {
			var st struct {
				Pause   bool `json:"pause"`
				Mute    bool `json:"mute"`
				Current *struct {
					Title string `json:"title"`
				} `json:"current"`
			}
			json.Unmarshal([]byte(out), &st)
			play, mute := "⏸", "🔇"
			if st.Pause {
				play = "▶"
			}
			if st.Mute {
				mute = "🔊"
			}
			title := "\x1b[2mnothing playing\x1b[0m"
			if st.Current != nil {
				title = st.Current.Title
			}
			line = link("previous", " ⏮ ") + link("toggle", " "+play+" ") + link("next", " ⏭ ") +
				link("mute", " "+mute+" ") + link("stop", " ⏹ ") + "  " + title
		}
		fmt.Print("\x1b[H\x1b[2K", line)
		time.Sleep(time.Second)
	}
}
