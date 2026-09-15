package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

type mpvMsg struct {
	Data      any    `json:"data"`
	Error     string `json:"error"`
	RequestID int    `json:"request_id"`
	Event     string `json:"event"`
	Reason    string `json:"reason"`
}

// Player drives one long-lived mpv process over its JSON IPC socket.
type Player struct {
	sock  string
	onEnd func(reason string)

	mu      sync.Mutex
	proc    *exec.Cmd
	conn    net.Conn
	nextID  int
	pending map[int]chan mpvMsg
}

func (p *Player) connect() error {
	if p.conn != nil {
		return nil
	}
	os.Remove(p.sock)
	proc := exec.Command("mpv", "--idle=yes", "--no-video", "--no-terminal",
		"--ytdl-format=bestaudio/best", "--input-ipc-server="+p.sock)
	if err := proc.Start(); err != nil {
		return fmt.Errorf("start mpv (is it installed?): %w", err)
	}
	var conn net.Conn
	var err error
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if conn, err = net.Dial("unix", p.sock); err == nil {
			break
		}
	}
	if err != nil {
		proc.Process.Kill()
		proc.Wait()
		return fmt.Errorf("connect to mpv: %w", err)
	}
	p.proc, p.conn, p.pending = proc, conn, map[int]chan mpvMsg{}
	go p.read(conn, proc)
	return nil
}

func (p *Player) read(conn net.Conn, proc *exec.Cmd) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m mpvMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.RequestID != 0 {
			p.mu.Lock()
			ch := p.pending[m.RequestID]
			delete(p.pending, m.RequestID)
			p.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		} else if m.Event == "end-file" && (m.Reason == "eof" || m.Reason == "error") {
			go p.onEnd(m.Reason)
		}
	}
	p.mu.Lock()
	if p.conn == conn {
		p.conn = nil
		for id, ch := range p.pending {
			close(ch)
			delete(p.pending, id)
		}
	}
	p.mu.Unlock()
	conn.Close()
	proc.Process.Kill()
	proc.Wait()
}

// Command sends an mpv IPC command, starting mpv if needed.
func (p *Player) Command(args ...any) (any, error) {
	p.mu.Lock()
	if err := p.connect(); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.nextID++
	id := p.nextID
	ch := make(chan mpvMsg, 1)
	p.pending[id] = ch
	b, _ := json.Marshal(map[string]any{"command": args, "request_id": id})
	_, err := p.conn.Write(append(b, '\n'))
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return nil, errors.New("mpv exited")
		}
		if m.Error != "success" {
			return nil, errors.New("mpv: " + m.Error)
		}
		return m.Data, nil
	case <-time.After(10 * time.Second):
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, errors.New("mpv did not respond")
	}
}

func (p *Player) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn != nil
}

func (p *Player) Quit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.proc != nil {
		p.proc.Process.Kill()
	}
	os.Remove(p.sock)
}
