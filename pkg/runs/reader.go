package runs

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Query reads owned JSONL files newest-first and returns a bounded page.
// Partial tails and unknown schemas are reported as warnings without
// echoing their contents. ctx cancellation is honored and always marked
// partial.
func Query(ctx context.Context, dir string, filter Filter, limit int, cursor *Cursor, wipeEpoch uint64) (QueryResult, error) {
	limit = clampLimit(limit)
	out := QueryResult{Records: []Record{}, Warnings: []Warning{}, WipeEpoch: wipeEpoch}
	if err := ctx.Err(); err != nil {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnTruncatedQuery, Count: 1, Message: "query canceled"})
		return out, err
	}
	if cursor != nil && cursor.WipeEpoch != wipeEpoch {
		return out, fmt.Errorf("cursor invalidated by wipe")
	}
	files, err := listOwnedFiles(dir)
	if err != nil {
		return out, err
	}
	if len(files) == 0 {
		if cursor != nil && cursor.SourceToken != "" {
			return out, fmt.Errorf("cursor invalidated by wipe")
		}
		return out, nil
	}
	token := sourceToken(files)
	if cursor != nil && cursor.SourceToken != "" && cursor.SourceToken != token {
		return out, fmt.Errorf("cursor invalidated by wipe")
	}
	return queryFromFiles(ctx, files, filter, limit, cursor, wipeEpoch, token)
}

func queryFromFiles(ctx context.Context, files []string, filter Filter, limit int, cursor *Cursor, wipeEpoch uint64, token string) (QueryResult, error) {
	limit = clampLimit(limit)
	out := QueryResult{Records: []Record{}, Warnings: []Warning{}, WipeEpoch: wipeEpoch}
	newestFirst := make([]string, len(files))
	copy(newestFirst, files)
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}

	var (
		malformed int
		unknown   int
		partial   int
		page      []Record
		canceled  bool
	)

	for _, path := range newestFirst {
		if err := ctx.Err(); err != nil {
			canceled = true
			break
		}
		recs, p, m, u, err := readFile(ctx, path)
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				canceled = true
				break
			}
			out.Partial = true
			out.Warnings = append(out.Warnings, Warning{Code: WarnUnreadable, Count: 1, Message: "history file unreadable"})
			continue
		}
		partial += p
		malformed += m
		unknown += u
		for i := len(recs) - 1; i >= 0; i-- {
			rec := recs[i]
			if cursor != nil && !olderThanCursor(rec, *cursor) {
				continue
			}
			if !filter.match(rec) {
				continue
			}
			page = append(page, rec)
			if len(page) == limit+1 {
				break
			}
		}
		if len(page) == limit+1 {
			break
		}
	}

	if len(page) > limit {
		last := page[limit-1]
		out.NextCursor = &Cursor{
			WipeEpoch:   wipeEpoch,
			SourceToken: token,
			ReturnedAt:  last.ReturnedAt,
			Sequence:    last.Sequence,
			AttemptID:   last.AttemptID,
		}
		page = page[:limit]
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
	if canceled {
		out.Partial = true
		out.Warnings = append(out.Warnings, Warning{Code: WarnTruncatedQuery, Count: 1, Message: "query canceled"})
		return out, ctx.Err()
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

func sourceToken(files []string) string {
	h := sha256.New()
	for _, f := range files {
		info, err := os.Lstat(f)
		if err != nil {
			fmt.Fprintf(h, "%s:missing\n", filepath.Base(f))
			continue
		}
		fmt.Fprintf(h, "%s:%d:%d\n", filepath.Base(f), info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
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
			return recs, partial, malformed, unknown, err
		}
		line, tooLong, hadNewline, readErr := readBoundedLine(r, MaxLineBytes)
		offset += int64(len(line))
		if tooLong {
			malformed++
			if readErr == io.EOF {
				if offset >= info.Size() && !hadNewline {
					partial++
					malformed--
				}
				break
			}
			if readErr != nil && readErr != io.EOF {
				return recs, partial, malformed, unknown, readErr
			}
			continue
		}
		if len(line) == 0 && readErr != nil {
			if readErr == io.EOF {
				break
			}
			return recs, partial, malformed, unknown, readErr
		}
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

func readBoundedLine(r *bufio.Reader, max int) (line []byte, tooLong, hadNewline bool, err error) {
	var buf []byte
	for {
		b, readErr := r.ReadByte()
		if readErr != nil {
			return buf, tooLong, hadNewline, readErr
		}
		if b == '\n' {
			if len(buf) > 0 && buf[len(buf)-1] == '\r' {
				buf = buf[:len(buf)-1]
			}
			return buf, tooLong, true, nil
		}
		if !tooLong {
			if len(buf) >= max {
				tooLong = true
				buf = nil
				continue
			}
			buf = append(buf, b)
		}
	}
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
	if err := refuseSymlinkParents(path); err != nil && !os.IsNotExist(err) {
		return QueryResult{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return QueryResult{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return QueryResult{}, fmt.Errorf("refusing symlink path")
	}
	if !info.IsDir() {
		token := sourceToken([]string{path})
		if cursor != nil && cursor.WipeEpoch != 0 {
			return QueryResult{}, fmt.Errorf("cursor invalidated by wipe")
		}
		if cursor != nil && cursor.SourceToken != "" && cursor.SourceToken != token {
			return QueryResult{}, fmt.Errorf("cursor invalidated by wipe")
		}
		return queryFromFiles(ctx, []string{path}, filter, limit, cursor, 0, token)
	}
	return Query(ctx, path, filter, limit, cursor, 0)
}

// EncodeCursor serializes a cursor as unpadded URL-safe base64 JSON.
func EncodeCursor(c Cursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses a cursor from unpadded URL-safe base64 JSON or raw JSON.
func DecodeCursor(s string) (Cursor, error) {
	var c Cursor
	if s == "" {
		return c, fmt.Errorf("empty cursor")
	}
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		if json.Unmarshal(raw, &c) == nil && (c.AttemptID != "" || c.Sequence != 0 || !c.ReturnedAt.IsZero() || c.SourceToken != "") {
			return c, nil
		}
	}
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return Cursor{}, err
	}
	return c, nil
}
