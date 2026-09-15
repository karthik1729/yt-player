// yt-player: YouTube Music over MCP. Playback runs in a background daemon
// so music keeps going after the MCP client disconnects.
//
//	yt-player          stdio MCP bridge (starts the daemon if needed)
//	yt-player daemon   background service owning mpv and the queue
//	yt-player call <tool> [json-args]   run one tool on the running daemon
//	yt-player controls   one-line player with clickable ytp:// links, for a terminal pane
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cache, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join(cache, "yt-player")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}
	sock := filepath.Join(dir, "daemon.sock")

	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		runDaemon(dir, sock)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "call" {
		if err := callTool(sock, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "controls" {
		controls(sock)
		return
	}
	if err := bridge(dir, sock); err != nil {
		log.Fatal(err)
	}
}

func bridge(dir, sock string) error {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		logf, err := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		cmd := exec.Command(exe, "daemon")
		cmd.Stdout, cmd.Stderr = logf, logf
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		cmd.Process.Release()
		logf.Close()
		for range 50 {
			time.Sleep(100 * time.Millisecond)
			if conn, err = net.Dial("unix", sock); err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("connect to daemon: %w", err)
		}
	}
	go func() {
		io.Copy(conn, os.Stdin)
		conn.(*net.UnixConn).CloseWrite()
	}()
	_, err = io.Copy(os.Stdout, conn)
	return err
}

// callTool runs one tool on a running daemon without starting it,
// for scripts and status bars: yt-player call <tool> [json-args]
func callTool(sock string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: yt-player call <tool> [json-args]")
	}
	var in map[string]any
	if len(args) > 1 {
		if err := json.Unmarshal([]byte(args[1]), &in); err != nil {
			return fmt.Errorf("args: %w", err)
		}
	}
	text, err := call(sock, args[0], in)
	if text != "" {
		fmt.Println(text)
	}
	return err
}

// call runs one tool on the running daemon and returns its text output.
func call(sock, tool string, in map[string]any) (string, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return "", errors.New("yt-player daemon is not running")
	}
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "yt-player-cli", Version: "1.0.0"}, nil)
	s, err := client.Connect(ctx, &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		return "", err
	}
	defer s.Close()
	r, err := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: in})
	if err != nil {
		return "", err
	}
	var text string
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text += t.Text
		}
	}
	if r.IsError {
		return text, errors.New("tool failed")
	}
	return text, nil
}

func runDaemon(dir, sock string) {
	if c, err := net.Dial("unix", sock); err == nil {
		c.Close()
		log.Fatal("daemon already running")
	}
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		log.Fatal(err)
	}
	os.Chmod(sock, 0o600)

	// Clients like GUI apps start us with a minimal PATH; mpv and the yt-dlp it runs usually live here.
	os.Setenv("PATH", os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin")

	d := newDaemon(filepath.Join(dir, "mpv.sock"))
	server := d.server()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		d.player.Quit()
		ln.Close()
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			defer conn.Close()
			ss, err := server.Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
			if err != nil {
				log.Print(err)
				return
			}
			ss.Wait()
		}()
	}
}
