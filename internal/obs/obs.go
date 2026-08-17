package obs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type Status struct {
	Live  func() bool
	Ready func() bool
	Stats func() map[string]any
}

type Server struct {
	Listen string
	Log    *slog.Logger
	Status Status

	ln   net.Listener
	srv  *http.Server
	addr atomic.Value
}

func (s *Server) Start() error {
	if s.Listen == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/live", s.live)
	mux.HandleFunc("/ready", s.ready)
	mux.HandleFunc("/metrics", s.metrics)
	s.srv = &http.Server{Addr: s.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}
	s.ln = ln
	s.addr.Store(ln.Addr().String())
	go func() { _ = s.srv.Serve(ln) }()
	if s.Log != nil {
		s.Log.Info("health listening", "addr", ln.Addr().String())
	}
	return nil
}

func (s *Server) Addr() string {
	v, _ := s.addr.Load().(string)
	return v
}

func (s *Server) Stop() {
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	if s.Status.Live != nil && !s.Status.Live() {
		http.Error(w, "dying", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	if s.Status.Ready != nil && !s.Status.Ready() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	stats := map[string]any{}
	if s.Status.Stats != nil {
		stats = s.Status.Stats()
	}
	for k, v := range stats {
		switch n := v.(type) {
		case int:
			fmt.Fprintf(w, "eggbot_%s %d\n", k, n)
		case int64:
			fmt.Fprintf(w, "eggbot_%s %d\n", k, n)
		case uint64:
			fmt.Fprintf(w, "eggbot_%s %d\n", k, n)
		case bool:
			nval := 0
			if n {
				nval = 1
			}
			fmt.Fprintf(w, "eggbot_%s %d\n", k, nval)
		default:
			fmt.Fprintf(w, "eggbot_%s %v\n", k, v)
		}
	}
}

func RedactMap(m map[string]any) {
	for k, v := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "pass") || strings.Contains(lk, "key") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") {
			m[k] = "[redacted]"
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			RedactMap(nested)
		}
	}
}

func JSONHandler(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
