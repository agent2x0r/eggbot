package obs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadyAndLive(t *testing.T) {
	ready := false
	s := &Server{Status: Status{
		Live:  func() bool { return true },
		Ready: func() bool { return ready },
		Stats: func() map[string]any { return map[string]any{"connected": true, "queue_depth": 2} },
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/live", s.live)
	mux.HandleFunc("/ready", s.ready)
	mux.HandleFunc("/metrics", s.metrics)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/live")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err, resp)
	}
	resp, _ = http.Get(ts.URL + "/ready")
	if resp.StatusCode != 503 {
		t.Fatal(resp.StatusCode)
	}
	ready = true
	resp, _ = http.Get(ts.URL + "/ready")
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/metrics")
	body, _ := io.ReadAll(resp.Body)
	if string(body) == "" {
		t.Fatal("empty metrics")
	}
}

func TestRedactMap(t *testing.T) {
	m := map[string]any{"password": "s3cret", "nick": "egg"}
	RedactMap(m)
	if m["password"] != "[redacted]" || m["nick"] != "egg" {
		t.Fatalf("%v", m)
	}
}
