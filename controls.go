package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Colors match a tokyonight tab bar: background, bright text, dim text, separators, accent.
const (
	bg     = "\x1b[48;2;36;40;59m"
	bright = "\x1b[38;2;192;202;245m"
	dim    = "\x1b[38;2;115;122;162m"
	faint  = "\x1b[38;2;86;95;137m"
	accent = "\x1b[38;2;122;162;247m"
	bold   = "\x1b[1m"
	unbold = "\x1b[22m"
)

// link wraps label in an OSC 8 hyperlink; the terminal decides what a click on ytp:// does.
func link(action, label string) string {
	return "\x1b]8;;ytp://" + action + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

// macOS: total cpu %, cpu count, memory size, page size, used pages. Elsewhere the meters are skipped.
const statsScript = `echo $(ps -A -o %cpu | awk '{s+=$1} END {printf "%.0f", s}') $(sysctl -n hw.ncpu hw.memsize hw.pagesize) $(vm_stat | awk '/Pages active|wired down|occupied by compressor/ {gsub(/\./,"",$NF); s+=$NF} END {print s}')`

var blocks = []rune("▁▂▃▄▅▆▇█")

// meter is a faint label plus a one-cell bar whose height and color show the level.
func meter(label string, pct float64) string {
	color := "158;206;106"
	switch {
	case pct >= 85:
		color = "247;118;142"
	case pct >= 60:
		color = "224;175;104"
	}
	i := min(max(int(math.Ceil(pct/100*8)), 1), 8) - 1
	return fmt.Sprintf("%s%s \x1b[38;2;%sm%c", faint, label, color, blocks[i])
}

// systemStats returns the meters and their visible width, or "" when unavailable.
func systemStats() (string, int) {
	out, err := exec.Command("sh", "-c", statsScript).Output()
	if err != nil {
		return "", 0
	}
	var cpu, ncpu, memsize, pagesize, pages float64
	if n, _ := fmt.Sscan(string(out), &cpu, &ncpu, &memsize, &pagesize, &pages); n < 5 || ncpu == 0 || memsize == 0 {
		return "", 0
	}
	return meter("cpu", min(100, cpu/ncpu)) + "  " + meter("ram", pages*pagesize/memsize*100), len("cpu x  ram x")
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n <= 1 {
		return ""
	}
	return string([]rune(s)[:n-1]) + "…"
}

const sep = faint + "  │  " // 5 cells

// controls redraws a one-line bar every second until killed:
// label on the left; song, buttons, cpu/ram meters and the clock on the right.
func controls(sock, label string) {
	fmt.Print("\x1b[?25l") // hide cursor
	var stats string
	var statsWidth int
	var statsAt time.Time
	for {
		cols := 80
		if ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil && ws.Col > 0 {
			cols = int(ws.Col)
		}

		left, leftWidth := "", 0
		if label != "" {
			left, leftWidth = " "+bold+accent+label+unbold, utf8.RuneCountInString(label)+1
		}

		// Right side, built from its end: clock, meters, buttons, then the song title in whatever room is left.
		clock := time.Now().Format("15:04")
		right, rightWidth := bold+bright+clock+unbold+"  ", len(clock)+2

		if time.Since(statsAt) >= 3*time.Second {
			stats, statsWidth = systemStats()
			statsAt = time.Now()
		}
		if stats != "" {
			right, rightWidth = stats+sep+right, statsWidth+5+rightWidth
		}

		title := ""
		if out, err := call(sock, "status", nil); err == nil {
			var st struct {
				Pause   bool `json:"pause"`
				Mute    bool `json:"mute"`
				Current *struct {
					Title string `json:"title"`
				} `json:"current"`
			}
			json.Unmarshal([]byte(out), &st)
			play, mute := "󰏤", "󰕾"
			if st.Pause {
				play = "󰐊"
			}
			if st.Mute {
				mute = "󰖁"
			}
			// Nerd Font icons are one cell each, so the width is exact: 5 buttons of 3 cells.
			buttons := dim + link("previous", " 󰒮 ") + link("toggle", " "+play+" ") + link("next", " 󰒭 ") +
				link("mute", " "+mute+" ") + link("stop", " 󰓛 ")
			right, rightWidth = buttons+sep+right, 15+5+rightWidth
			if st.Current != nil {
				title = st.Current.Title
			} else {
				title = "nothing playing"
			}
		}
		title = truncate(title, min(40, cols-leftWidth-rightWidth-4))
		if title != "" {
			right, rightWidth = bright+title+" "+right, utf8.RuneCountInString(title)+1+rightWidth
		}

		gap := max(cols-leftWidth-rightWidth, 0)
		fmt.Print("\x1b[H", bg, "\x1b[2K", left, strings.Repeat(" ", gap), right)
		time.Sleep(time.Second)
	}
}
