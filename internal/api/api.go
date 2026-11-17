package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/kshaikh/scada/internal/core/alarm"
	"github.com/kshaikh/scada/internal/core/db"
	"github.com/kshaikh/scada/internal/core/event"
	"github.com/kshaikh/scada/internal/core/history"
	"github.com/kshaikh/scada/internal/core/tag"
)

type API struct {
	DB      *db.DB
	Alarms  *alarm.Manager
	History *history.Store
	Bus     *event.Bus
	DriverWrite func(tagID string, value any) error
	WebFS   http.FileSystem
}

func New(a *API) *API { return a }

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags", a.listTags)
	mux.HandleFunc("GET /api/tags/{id}", a.getTag)
	mux.HandleFunc("POST /api/tags/{id}/value", a.setTag)
	mux.HandleFunc("GET /api/alarms", a.listAlarms)
	mux.HandleFunc("POST /api/alarms/{id}/ack", a.ackAlarm)
	mux.HandleFunc("GET /api/history", a.getHistory)
	mux.HandleFunc("GET /api/events", a.events)
	mux.HandleFunc("GET /api/version", a.version)
	// static web
	if a.WebFS != nil {
		mux.Handle("/", http.FileServer(a.WebFS))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("SCADA running"))
		})
	}
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (a *API) listTags(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.DB.Snapshot())
}

func (a *API) getTag(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, ok := a.DB.Get(id)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, 200, t.Snapshot())
}

func (a *API) setTag(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Value any `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	t, ok := a.DB.Get(id)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	if t.Access != "rw" {
		http.Error(w, "read-only tag", 403)
		return
	}
	v, err := t.Coerce(body.Value)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if a.DriverWrite != nil {
		_ = a.DriverWrite(id, v)
	}
	if err := a.DB.Set(id, v, tag.QualityGood); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if a.History != nil {
		if t2, _ := a.DB.Get(id); t2 != nil {
			a.History.Record(id, t2.Snapshot())
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) listAlarms(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	writeJSON(w, 200, a.Alarms.Alarms(state))
}

func (a *API) ackAlarm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.Alarms.Ack(id); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) getHistory(w http.ResponseWriter, r *http.Request) {
	tagID := r.URL.Query().Get("tag")
	if tagID == "" {
		http.Error(w, "tag required", 400)
		return
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	limitStr := r.URL.Query().Get("limit")
	var from, to time.Time
	now := time.Now()
	if fromStr == "" {
		from = now.Add(-time.Hour)
	} else {
		from = parseTime(fromStr)
	}
	if toStr == "" {
		to = now
	} else {
		to = parseTime(toStr)
	}
	limit := 500
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			limit = n
		}
	}
	if a.History == nil {
		writeJSON(w, 200, []any{})
		return
	}
	snaps, err := a.History.Query(tagID, from, to, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, snaps)
}

func parseTime(s string) time.Time {
	// try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms)
	}
	return time.Now()
}

func (a *API) version(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{"version": "0.1.1"}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info["commit"] = s.Value
			case "vcs.time":
				info["built"] = s.Value
			case "vcs.modified":
				info["modified"] = s.Value == "true"
			}
		}
	}
	writeJSON(w, 200, info)
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	ch := a.Bus.Subscribe("*")
	defer a.Bus.Unsubscribe(ch)
	// send initial snapshot
	data, _ := json.Marshal(map[string]any{"type": "snapshot", "tags": a.DB.Snapshot()})
	fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			switch ev.Topic {
			case "alarm":
				b, _ := json.Marshal(map[string]any{"type": "alarm", "alarm": ev.Data})
				fmt.Fprintf(w, "event: alarm\ndata: %s\n\n", b)
			default:
				if strings.HasPrefix(ev.Topic, "tag:") && ev.Topic != "tag:*" {
					b, _ := json.Marshal(map[string]any{"type": "tag", "tag": ev.Data})
					fmt.Fprintf(w, "event: tag\ndata: %s\n\n", b)
				}
			}
			flusher.Flush()
		}
	}
}
