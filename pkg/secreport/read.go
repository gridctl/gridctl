package secreport

import (
	"context"
	"io"
	"os"
)

func readBoundedFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrSourceUnreadable
	}
	if !info.Mode().IsRegular() {
		return nil, ErrSourceUnreadable
	}
	if info.Size() > limit {
		return nil, ErrSourceInvalidFmt
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrSourceUnreadable
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, ErrSourceUnreadable
	}
	if int64(len(data)) > limit {
		return nil, ErrSourceInvalidFmt
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
