package runs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// Query reads owned JSONL files newest-first and returns a bounded page.
// Partial tails and unknown schemas are reported as warnings without
// echoing their contents. ctx cancellation is honored between records.
func Query(ctx context.Context, dir string, filter Filter, limit int, cursor *Cursor, wipeEpoch uint64) (QueryResult, error) {
	limit = clampLimit(limit)
	out := QueryResult{Records: []Record{}, Warnings: []Warning{}, WipeEpoch: wipeEpoch}
	if cursor != nil && cursor.WipeEpoch != wipeEpoch {
		return out, fmt.Errorf("cursor invalidated by wipe")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	files, err := listOwnedFiles(dir)
	if err != nil {
		return out, err
	}
	if len(files) == 0 {
		return out, nil
	}

	var (
		malformed int
		unknown   int
		partial   int
		matched   []Record
	)

	// Read oldest-to-newest then reverse so ordering is returned_at desc,
	// sequence desc, attempt_id desc.
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			out.Partial = true
			out.Warnings = append(out.Warnings, Warning{Code: WarnTruncatedQuery, Count: 1, Message: "query canceled"})
			break
		}
		recs, p, m, u, err := readFile(ctx, path)
		if err != nil {
			out.Partial = true
			out.Warnings = append(out.Warnings, Warning{Code: WarnUnreadable, Count: 1, Message: "history file unreadable"})
			continue
		}
		partial += p
		malformed += m
		unknown += u
		matched = append(matched, recs...)
	}

	sort.SliceStable(matched, func(i, j int) bool {
		if !matched[i].ReturnedAt.Equal(matched[j].ReturnedAt) {
			return matched[i].ReturnedAt.After(matched[j].ReturnedAt)
		}
		if matched[i].Sequence != matched[j].Sequence {
			return matched[i].Sequence > matched[j].Sequence
		}
		return matched[i].AttemptID > matched[j].AttemptID
	})

	var page []Record
	for _, rec := range matched {
		if cursor != nil && !olderThanCursor(rec, *cursor) {
			continue
		}
		if !filter.match(rec) {
			continue
		}
		page = append(page, rec)
		if len(page) == limit {
			if hasMore(matched, rec, filter, cursor) {
				out.NextCursor = &Cursor{
					WipeEpoch:  wipeEpoch,
					ReturnedAt: rec.ReturnedAt,
					Sequence:   rec.Sequence,
					AttemptID:  rec.AttemptID,
				}
			}
			break
		}
	}
	out.Records = page
	if partial > 0 {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnPartialTail, Count: partial, Message: "incomplete final line omitted"})
	}
	if malformed > 0 {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnMalformed, Count: malformed, Message: "malformed records omitted"})
	}
	if unknown > 0 {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnUnsupportedSchema, Count: unknown, Message: "unsupported schema versions omitted"})
	}
	return out, nil
}

func olderThanCursor(rec Record, c Cursor) bool {
	if rec.ReturnedAt.Before(c.ReturnedAt) {
		return true
	}
	if rec.ReturnedAt.After(c.ReturnedAt) {
		return false
	}
	if rec.Sequence < c.Sequence {
		return true
	}
	if rec.Sequence > c.Sequence {
		return false
	}
	return rec.AttemptID < c.AttemptID
}

func hasMore(matched []Record, last Record, filter Filter, cursor *Cursor) bool {
	seenLast := false
	for _, rec := range matched {
		if !seenLast {
			if rec.AttemptID == last.AttemptID && rec.Sequence == last.Sequence {
				seenLast = true
			}
			continue
		}
		if cursor != nil && !olderThanCursor(rec, *cursor) {
			continue
		}
		if filter.match(rec) {
			return true
		}
	}
	return false
}

func readFile(ctx context.Context, path string) (recs []Record, partial, malformed, unknown int, err error) {
	if err := refuseSymlink(path); err != nil {
		return nil, 0, 0, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, 0, 0, err
	}
	r := bufio.NewReaderSize(f, 64*1024)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return recs, partial, malformed, unknown, nil
		}
		line, readErr := r.ReadBytes('\n')
		offset += int64(len(line))
		if len(line) == 0 && readErr != nil {
			if readErr == io.EOF {
				break
			}
			return recs, partial, malformed, unknown, readErr
		}
		hadNewline := bytes.HasSuffix(line, []byte("\n"))
		line = bytes.TrimRight(line, "\r\n")
		if !hadNewline {
			if offset >= info.Size() || readErr == io.EOF {
				if len(line) > 0 {
					partial++
				}
				break
			}
		}
		if len(line) == 0 {
			if readErr == io.EOF {
				break
			}
			continue
		}
		if len(line) > MaxLineBytes {
			malformed++
			if readErr == io.EOF {
				break
			}
			continue
		}
		rec, code, ok := parseRecord(line)
		if !ok {
			switch code {
			case WarnUnsupportedSchema:
				unknown++
			default:
				malformed++
			}
		} else {
			recs = append(recs, rec)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return recs, partial, malformed, unknown, readErr
		}
	}
	return recs, partial, malformed, unknown, nil
}

func parseRecord(line []byte) (Record, string, bool) {
	var probe struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return Record{}, WarnMalformed, false
	}
	if probe.SchemaVersion != SchemaVersion {
		return Record{}, WarnUnsupportedSchema, false
	}
	var rec Record
	if err := json.Unmarshal(line, &rec); err != nil {
		return Record{}, WarnMalformed, false
	}
	if rec.AttemptID == "" || rec.Disposition == "" {
		return Record{}, WarnMalformed, false
	}
	return rec, "", true
}

// QueryPath is an explicit offline file or directory query. It never
// consults a live recorder.
func QueryPath(ctx context.Context, path string, filter Filter, limit int, cursor *Cursor) (QueryResult, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return QueryResult{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return QueryResult{}, fmt.Errorf("refusing symlink path")
	}
	if !info.IsDir() {
		return queryFiles(ctx, []string{path}, filter, limit, cursor, 0)
	}
	return Query(ctx, path, filter, limit, cursor, 0)
}

func queryFiles(ctx context.Context, files []string, filter Filter, limit int, cursor *Cursor, wipeEpoch uint64) (QueryResult, error) {
	limit = clampLimit(limit)
	out := QueryResult{Records: []Record{}, Warnings: []Warning{}, WipeEpoch: wipeEpoch}
	if cursor != nil && cursor.WipeEpoch != wipeEpoch {
		return out, fmt.Errorf("cursor invalidated by wipe")
	}
	var matched []Record
	var partial, malformed, unknown int
	for _, path := range files {
		recs, p, m, u, err := readFile(ctx, path)
		if err != nil {
			out.Partial = true
			out.Warnings = append(out.Warnings, Warning{Code: WarnUnreadable, Count: 1, Message: "history file unreadable"})
			continue
		}
		partial += p
		malformed += m
		unknown += u
		matched = append(matched, recs...)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if !matched[i].ReturnedAt.Equal(matched[j].ReturnedAt) {
			return matched[i].ReturnedAt.After(matched[j].ReturnedAt)
		}
		if matched[i].Sequence != matched[j].Sequence {
			return matched[i].Sequence > matched[j].Sequence
		}
		return matched[i].AttemptID > matched[j].AttemptID
	})
	var page []Record
	for _, rec := range matched {
		if !filter.match(rec) {
			continue
		}
		page = append(page, rec)
		if len(page) == limit {
			break
		}
	}
	out.Records = page
	if partial > 0 {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnPartialTail, Count: partial, Message: "incomplete final line omitted"})
	}
	if malformed > 0 {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnMalformed, Count: malformed, Message: "malformed records omitted"})
	}
	if unknown > 0 {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnUnsupportedSchema, Count: unknown, Message: "unsupported schema versions omitted"})
	}
	return out, nil
}
