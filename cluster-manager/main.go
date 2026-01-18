package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/websocket"
)

type nodeInfo struct {
	Hostname  string `json:"hostname"`
	Uptime    string `json:"uptime"`
	CronCount int    `json:"cron_count"`
}

var (
	sharedSecret string
	dataDir      string
	hostRoot     string
)

func main() {
	addr := flag.String("addr", ":9090", "HTTP listen address")
	flag.StringVar(&dataDir, "data-dir", "/var/lib/cluster-manager", "Data directory for cron storage")
	flag.StringVar(&hostRoot, "host-root", "/host", "Host filesystem root mount")
	flag.Parse()

	sharedSecret = os.Getenv("CLUSTER_MANAGER_SECRET")
	if sharedSecret == "" {
		log.Println("WARNING: CLUSTER_MANAGER_SECRET not set, authentication disabled")
	}

	if err := initCronStorage(); err != nil {
		log.Fatalf("failed to init cron storage: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /info", authMiddleware(handleInfo))
	mux.HandleFunc("GET /cron", authMiddleware(handleCronList))
	mux.HandleFunc("POST /cron", authMiddleware(handleCronCreate))
	mux.HandleFunc("PUT /cron/{id}", authMiddleware(handleCronUpdate))
	mux.HandleFunc("DELETE /cron/{id}", authMiddleware(handleCronDelete))
	mux.HandleFunc("POST /cron/{id}/run", authMiddleware(handleCronRun))
	mux.Handle("/terminal", websocket.Handler(handleTerminal))

	log.Printf("cluster-manager listening on %s", *addr)
	if err := http.ListenAndServe(*addr, logRequests(mux)); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	uptime := getUptime()
	cronCount := getCronCount()

	writeJSON(w, nodeInfo{
		Hostname:  hostname,
		Uptime:    uptime,
		CronCount: cronCount,
	})
}

func getUptime() string {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return "unknown"
	}
	uptime := time.Duration(info.Uptime) * time.Second
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	mins := int(uptime.Minutes()) % 60
	if days > 0 {
		return strings.TrimSpace(strings.Join([]string{
			intToStr(days) + "d",
			intToStr(hours) + "h",
			intToStr(mins) + "m",
		}, " "))
	}
	if hours > 0 {
		return intToStr(hours) + "h " + intToStr(mins) + "m"
	}
	return intToStr(mins) + "m"
}

func intToStr(n int) string {
	return strings.TrimPrefix(strings.TrimPrefix(string(rune('0'+n/10))+string(rune('0'+n%10)), "0"), "")
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sharedSecret != "" {
			provided := r.Header.Get("X-Cluster-Manager-Secret")
			if provided != sharedSecret {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func handleTerminal(ws *websocket.Conn) {
	// Verify auth for WebSocket
	if sharedSecret != "" {
		provided := ws.Request().Header.Get("X-Cluster-Manager-Secret")
		if provided != sharedSecret {
			ws.Close()
			return
		}
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Use chroot to run shell in host filesystem
	cmd := exec.Command("chroot", hostRoot, shell, "-l")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"HOME=/root",
		"USER=root",
	)

	runTerminalSession(ws, cmd)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		log.Printf("%s %s %s", r.Method, r.URL.Path, duration.Round(time.Millisecond))
	})
}
