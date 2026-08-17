package obs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartStopAndJSON(t *testing.T) {
	s := &Server{
		Listen: "127.0.0.1:0",
		Status: Status{
			Live:  func() bool { return false },
			Ready: func() bool { return true },
			Stats: func() map[string]any {
				return map[string]any{"n": 1, "n64": int64(2), "u": uint64(3), "ok": true, "s": "x"}
			},
		},
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if s.Addr() == "" {
		t.Fatal("addr")
	}
	resp, err := http.Get("http://" + s.Addr() + "/live")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 {
		t.Fatal(resp.StatusCode)
	}
	resp, err = http.Get("http://" + s.Addr() + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	empty := &Server{}
	if err := empty.Start(); err != nil {
		t.Fatal(err)
	}
	empty.Stop()
	w := httptest.NewRecorder()
	JSONHandler(w, 200, map[string]any{"ok": true})
	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil || got["ok"] != true {
		t.Fatal(got, err)
	}
	nested := map[string]any{"outer": map[string]any{"token": "s"}}
	RedactMap(nested)
	_ = time.Millisecond
}
