package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
)

type runRecordDTO struct {
	SchemaVersion      int    `json:"schemaVersion"`
	RecorderInstanceID string `json:"recorderInstanceId"`
	Sequence           uint64 `json:"sequence"`
	AttemptID          string `json:"attemptId"`
	ParentAttemptID    string `json:"parentAttemptId,omitempty"`
	RootAttemptID      string `json:"rootAttemptId,omitempty"`
	PreviousAttemptID  string `json:"previousAttemptId,omitempty"`
	StartedAt          string `json:"startedAt"`
	ReturnedAt         string `json:"returnedAt"`
	DurationMS         int64  `json:"durationMs"`
	DownstreamMS       int64  `json:"downstreamDurationMs,omitempty"`
	RequestedName      string `json:"requestedName,omitempty"`
	RequestedOmitted   string `json:"requestedNameOmitted,omitempty"`
	ResolvedServer     string `json:"resolvedServer,omitempty"`
	ResolvedServerOmit string `json:"resolvedServerOmitted,omitempty"`
	ResolvedTool       string `json:"resolvedTool,omitempty"`
	ResolvedToolOmit   string `json:"resolvedToolOmitted,omitempty"`
	Disposition        string `json:"disposition"`
	Stage              string `json:"stage"`
	Reason             string `json:"reason"`
	ReplicaID          *int   `json:"replicaId,omitempty"`
	TraceID            string `json:"traceId,omitempty"`
	ClientLabel        string `json:"clientLabel,omitempty"`
	AccessLabel        string `json:"accessLabel,omitempty"`
}

type runListDTO struct {
	Records    []runRecordDTO `json:"records"`
	Warnings   []runs.Warning `json:"warnings"`
	Partial    bool           `json:"partial"`
	NextCursor string         `json:"nextCursor,omitempty"`
	WipeEpoch  uint64         `json:"wipeEpoch"`
}

type runStatusDTO struct {
	runs.Status
	Inventory     runs.Inventory `json:"inventory"`
	RecordingNote string         `json:"recordingNote"`
	PrivacyNote   string         `json:"privacyNote"`
	RetentionNote string         `json:"retentionNote"`
}

const runsRecordingNote = "Records are saved after dispatch attempts return. Recording is best-effort. Attempts interrupted by a crash may leave no record, and older records may have been removed by retention or wipe."
const runsPrivacyNote = "Records store metadata only: generated IDs, times, bounded names, and disposition. Argument and result values are not stored. Names and caller-declared labels may still be sensitive."
const runsRetentionNote = "Default retention is seven days and 100 MiB of logical record bytes per stack, including rotated files. Physical filesystem overhead is extra. Wipe is not secure erasure and does not remove exports or backups."

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
		c, err := decodeRunCursor(raw)
		if err != nil {
			writeJSONError(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		cursor = &c
	}
	epoch := uint64(0)
	if s.runRecorder != nil {
		epoch = s.runRecorder.WipeEpoch()
	}
	res, err := runs.Query(r.Context(), dir, filter, limit, cursor, epoch)
	if err != nil {
		if err.Error() == "cursor invalidated by wipe" {
			writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSONError(w, "Failed to read run history", http.StatusInternalServerError)
		return
	}
	dto := runListDTO{
		Records:   make([]runRecordDTO, 0, len(res.Records)),
		Warnings:  res.Warnings,
		Partial:   res.Partial,
		WipeEpoch: res.WipeEpoch,
	}
	if dto.Warnings == nil {
		dto.Warnings = []runs.Warning{}
	}
	for _, rec := range res.Records {
		dto.Records = append(dto.Records, runToDTO(rec))
	}
	if res.NextCursor != nil {
		dto.NextCursor = encodeRunCursor(*res.NextCursor)
	}
	if export {
		w.Header().Set("Content-Disposition", "attachment; filename=\"runs.json\"")
	}
	writeJSON(w, dto)
}

func (s *Server) handleRunsStatus(w http.ResponseWriter, r *http.Request) {
	if s.stackName == "" {
		writeJSONError(w, "No stack loaded", http.StatusServiceUnavailable)
		return
	}
	st := runs.Status{Enabled: false, Effective: false, WriterHealth: runs.HealthStopped, HistoricalLoss: runs.LossUnknown, Drops: map[string]uint64{}, Failures: map[string]uint64{}}
	if s.runRecorder != nil {
		st = s.runRecorder.Status()
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
		RetentionNote: runsRetentionNote,
	})
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
	enabled := s.runRecorder != nil && s.runRecorder.Status().Enabled
	if err != nil {
		writeJSON(w, map[string]any{
			"success":          false,
			"partial":          true,
			"recordingEnabled": enabled,
			"error":            "wipe incomplete",
			"scope":            s.stackName,
		})
		return
	}
	writeJSON(w, map[string]any{
		"success":          true,
		"partial":          false,
		"recordingEnabled": enabled,
		"scope":            s.stackName,
		"note":             "Wipe is not secure erasure and does not remove exports or backups.",
	})
}

func (s *Server) runsDir() (string, error) {
	if s.runRecorder != nil && s.runRecorder.DirPath() != "" {
		return s.runRecorder.DirPath(), nil
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

func runToDTO(rec runs.Record) runRecordDTO {
	dto := runRecordDTO{
		SchemaVersion:      rec.SchemaVersion,
		RecorderInstanceID: rec.RecorderInstanceID,
		Sequence:           rec.Sequence,
		AttemptID:          rec.AttemptID,
		ParentAttemptID:    rec.ParentAttemptID,
		RootAttemptID:      rec.RootAttemptID,
		PreviousAttemptID:  rec.PreviousAttemptID,
		DurationMS:         rec.DurationMS,
		DownstreamMS:       rec.DownstreamMS,
		RequestedName:      rec.RequestedName,
		RequestedOmitted:   rec.RequestedOmitted,
		ResolvedServer:     rec.ResolvedServer,
		ResolvedServerOmit: rec.ResolvedServerOmit,
		ResolvedTool:       rec.ResolvedTool,
		ResolvedToolOmit:   rec.ResolvedToolOmit,
		Disposition:        rec.Disposition,
		Stage:              rec.Stage,
		Reason:             rec.Reason,
		ReplicaID:          rec.ReplicaID,
		TraceID:            rec.TraceID,
		ClientLabel:        rec.ClientLabel,
		AccessLabel:        rec.AccessLabel,
	}
	if !rec.StartedAt.IsZero() {
		dto.StartedAt = rec.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !rec.ReturnedAt.IsZero() {
		dto.ReturnedAt = rec.ReturnedAt.UTC().Format(time.RFC3339Nano)
	}
	return dto
}

func encodeRunCursor(c runs.Cursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeRunCursor(s string) (runs.Cursor, error) {
	var c runs.Cursor
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}
