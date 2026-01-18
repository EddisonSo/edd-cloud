package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"golang.org/x/net/websocket"
)

func runTerminalSession(ws *websocket.Conn, cmd *exec.Cmd) {
	// Start the command with a pseudo-terminal
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("failed to start pty: %v", err)
		ws.Write([]byte("Failed to start terminal: " + err.Error() + "\r\n"))
		ws.Close()
		return
	}
	defer ptmx.Close()

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Copy pty output to websocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := ptmx.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("pty read error: %v", err)
					}
					return
				}
				if n > 0 {
					if _, err := ws.Write(buf[:n]); err != nil {
						log.Printf("ws write error: %v", err)
						return
					}
				}
			}
		}
	}()

	// Copy websocket input to pty
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ws.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("ws read error: %v", err)
				}
				close(done)
				cmd.Process.Signal(os.Interrupt)
				return
			}
			if n > 0 {
				// Check for resize message (JSON format)
				if buf[0] == '{' {
					handleResize(ptmx, buf[:n])
					continue
				}
				if _, err := ptmx.Write(buf[:n]); err != nil {
					log.Printf("pty write error: %v", err)
					return
				}
			}
		}
	}()

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		log.Printf("command finished with error: %v", err)
	}

	// Cleanup
	close(done)
	ws.Close()
	wg.Wait()
}

func handleResize(ptmx *os.File, data []byte) {
	// Simple JSON parsing for resize messages: {"cols":80,"rows":24}
	var cols, rows uint16 = 80, 24

	// Very basic parsing - in production use json.Unmarshal
	str := string(data)
	if len(str) > 10 {
		// Parse cols
		for i := 0; i < len(str)-5; i++ {
			if str[i:i+6] == "\"cols\"" {
				j := i + 7
				for j < len(str) && str[j] == ' ' || str[j] == ':' {
					j++
				}
				num := uint16(0)
				for j < len(str) && str[j] >= '0' && str[j] <= '9' {
					num = num*10 + uint16(str[j]-'0')
					j++
				}
				if num > 0 {
					cols = num
				}
			}
			if str[i:i+6] == "\"rows\"" {
				j := i + 7
				for j < len(str) && str[j] == ' ' || str[j] == ':' {
					j++
				}
				num := uint16(0)
				for j < len(str) && str[j] >= '0' && str[j] <= '9' {
					num = num*10 + uint16(str[j]-'0')
					j++
				}
				if num > 0 {
					rows = num
				}
			}
		}
	}

	pty.Setsize(ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}
