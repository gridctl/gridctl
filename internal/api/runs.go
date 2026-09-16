package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
)

type runStatusDTO struct {
	runs.Status
	Inventory     runs.Inventory `json:"inventory"`
	RecordingNote string         `json:"recordingNote"`
	PrivacyNote   string         `json:"privacyNote"`
	RetentionNote string         `json:"retentionNote"`
}

const runsRecordingNote = "Records are saved after dispatch attempts return. Recording is best-effort. Attempts interrupted by a crash may leave no record, and older records may have been removed by retention or wipe."
const runsPrivacyNote = "Records store metadata only: generated IDs, times, bounded names, and disposition. Argument and result values are not stored. Names and caller-declared labels may still be sensitive."

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	s.serveRuns(w, r, false)
}

func (s *Server) handleRunsExport(w http.ResponseWriter, r *http.Request) {
	s.serveRuns(w, r, true)
}

func (s *Server) serveRuns(w http.ResponseWriter, r *http.Request, export bool) {
	if s.stackName == "" {
		writeJSONError(w, "No stack loaded", http.StatusServiceUnavailable)
		return
	}
	dir, err := s.runsDir()
	if err != nil {
		writeJSONError(w, "Failed to locate run history", http.StatusInternalServerError)
		return
	}
	filter, err := parseRunFilter(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var cursor *runs.Cursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		c, err := runs.DecodeCursor(raw)
		if err != nil {
			writeJSONError(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		cursor = &c
	}
	epoch := uint64(0)
	if rec := s.getRunRecorder(); rec != nil {
		epoch = rec.WipeEpoch()
	}
	res, err := runs.Query(r.Context(), dir, filter, limit, cursor, epoch)
	if err != nil {
		if err.Error() == "cursor invalidated by wipe" {
			writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		if res.Partial {
			env := runs.EnvelopeFromResult(res, runs.QuerySource{Kind: "live", Stack: s.stackName, Path: dir}, filter)
			if export {
				w.Header().Set("Content-Disposition", "attachment; filename=\"runs.json\"")
			}
			writeJSON(w, env)
			return
		}
		writeJSONError(w, "Failed to read run history", http.StatusInternalServerError)
		return
	}
	env := runs.EnvelopeFromResult(res, runs.QuerySource{Kind: "live", Stack: s.stackName, Path: dir}, filter)
	if export {
		w.Header().Set("Content-Disposition", "attachment; filename=\"runs.json\"")
	}
	writeJSON(w, env)
}

func (s *Server) handleRunsStatus(w http.ResponseWriter, r *http.Request) {
	if s.stackName == "" {
		writeJSONError(w, "No stack loaded", http.StatusServiceUnavailable)
		return
	}
	st := runs.Status{Enabled: false, Effective: false, WriterHealth: runs.HealthUnknown, HistoricalLoss: runs.LossUnknown, Known: false, Drops: map[string]uint64{}, Failures: map[string]uint64{}}
	if rec := s.getRunRecorder(); rec != nil {
		st = rec.Status()
	}
	inv, err := runs.StackInventory(s.stackName)
	if err != nil {
		writeJSONError(w, "Failed to read run inventory", http.StatusInternalServerError)
		return
	}
	writeJSON(w, runStatusDTO{
		Status:        st,
		Inventory:     inv,
		RecordingNote: runsRecordingNote,
		PrivacyNote:   runsPrivacyNote,
		RetentionNote: retentionNote(st),
	})
}

func retentionNote(st runs.Status) string {
	days := st.RetentionMaxAgeDays
	if days <= 0 {
		days = 7
	}
	mb := st.RetentionMaxBytes / (1024 * 1024)
	if mb <= 0 {
		mb = 100
	}
	return fmt.Sprintf("Configured retention is %d days and %d MiB of logical record bytes per stack, including the active file and rotated segments. Physical filesystem overhead is extra. Wipe is not secure erasure and does not remove exports or backups.", days, mb)
}

func (s *Server) handleRunsWipe(w http.ResponseWriter, r *http.Request) {
	if s.stackName == "" {
		writeJSONError(w, "No stack loaded", http.StatusServiceUnavailable)
		return
	}
	if server := r.URL.Query().Get("server"); server != "" {
		writeJSONError(w, runs.ErrPerServerRuns.Error(), http.StatusBadRequest)
		return
	}
	err := runs.WipeStack(r.Context(), s.stackName)
	enabled := false
	if rec := s.getRunRecorder(); rec != nil {
		enabled = rec.Status().Enabled
	}
	if err != nil {
		writeJSON(w, map[string]any{
			"success":           false,
			"partial":           true,
			"recording_enabled": enabled,
			"recordingEnabled":  enabled,
			"error":             "wipe incomplete",
			"scope":             s.stackName,
		})
		return
	}
	writeJSON(w, map[string]any{
		"success":           true,
		"partial":           false,
		"recording_enabled": enabled,
		"recordingEnabled":  enabled,
		"scope":             s.stackName,
		"note":              "Wipe is not secure erasure and does not remove exports or backups.",
	})
}

func (s *Server) runsDir() (string, error) {
	if rec := s.getRunRecorder(); rec != nil && rec.DirPath() != "" {
		return rec.DirPath(), nil
	}
	return runs.Dir(s.stackName)
}

func parseRunFilter(r *http.Request) (runs.Filter, error) {
	q := r.URL.Query()
	f := runs.Filter{
		RequestedName:     q.Get("requested"),
		ResolvedServer:    q.Get("server"),
		ResolvedTool:      q.Get("tool"),
		Disposition:       q.Get("disposition"),
		ClientLabel:       q.Get("client"),
		AccessLabel:       q.Get("access"),
		AttemptID:         q.Get("attempt"),
		ParentAttemptID:   q.Get("parent"),
		RootAttemptID:     q.Get("root"),
		PreviousAttemptID: q.Get("previous"),
		TraceID:           q.Get("trace"),
	}
	var err error
	if v := q.Get("since"); v != "" {
		f.Since, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errors.New("invalid since")
		}
	}
	if v := q.Get("until"); v != "" {
		f.Until, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errors.New("invalid until")
		}
	}
	return f, nil
}
