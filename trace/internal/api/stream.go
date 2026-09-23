package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// apiStream handles GET /api/stream — a server-sent events feed for
// real-time dashboard updates. It pushes an overview snapshot every few
// seconds plus heartbeat comments so proxies keep the connection open.
// Clients fall back to polling when the stream is unavailable.
func (s *Server) apiStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "no_stream", "streaming unsupported by connection", false)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx and friends) so events flush immediately.
	w.Header().Set("X-Accel-Buffering", "no")

	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	send := func(event string, payload any) bool {
		b, err := json.Marshal(payload)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}

	if !send("hello", map[string]any{"ok": true, "ts": time.Now().UTC()}) {
		return
	}

	snapshot := func() bool {
		ov, err := s.Store.Overview(r.Context(), p.ID)
		if err != nil {
			return send("error", map[string]any{"message": err.Error()})
		}
		ov["project"] = p
		return send("overview", ov)
	}

	// First snapshot immediately so a fresh tab paints without waiting a tick.
	if !snapshot() {
		return
	}

	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if !snapshot() {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
