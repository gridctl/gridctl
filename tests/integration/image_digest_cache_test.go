//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
)

func TestImageDigestCache_RealRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	rt, err := dockerruntime.NewWithInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const tagged = "alpine:3.22"
	if err := rt.EnsureImage(ctx, tagged); err != nil {
		t.Fatalf("EnsureImage(%s): %v", tagged, err)
	}

	summaries, err := rt.Client().ImageList(ctx, image.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	digestRef := ""
	tagDigestRef := ""
	for _, img := range summaries {
		for _, rd := range img.RepoDigests {
			if strings.HasPrefix(rd, "alpine@sha256:") || strings.Contains(rd, "/alpine@sha256:") {
				digestRef = rd
				if at := strings.Index(rd, "@"); at >= 0 {
					tagDigestRef = tagged + rd[at:]
				}
				break
			}
		}
		if digestRef != "" {
			break
		}
	}
	if digestRef == "" {
		t.Fatal("pulled alpine:3.22 has no RepoDigests; cannot verify digest cache")
	}

	exists, err := dockerruntime.ImageExists(ctx, rt.Client(), digestRef)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("ImageExists(%s) = false after tag pull", digestRef)
	}
	if tagDigestRef != "" {
		exists, err = dockerruntime.ImageExists(ctx, rt.Client(), tagDigestRef)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("ImageExists(%s) = false after tag pull", tagDigestRef)
		}
	}

	wrong := tagged + "@sha256:" + strings.Repeat("0", 64)
	exists, err = dockerruntime.ImageExists(ctx, rt.Client(), wrong)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("ImageExists(%s) matched on tag ignoring digest", wrong)
	}

	if err := dockerruntime.EnsureImage(ctx, rt.Client(), digestRef, logging.NewDiscardLogger()); err != nil {
		t.Fatalf("EnsureImage digest ref: %v", err)
	}
}
