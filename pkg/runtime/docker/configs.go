package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/runtime"

	"github.com/docker/docker/api/types/container"
)

// materializeConfigs copies regular-file entries into a created container.
// Parent directories are left to the engine. A directory entry for a path
// that already exists in the image resets that directory's mode, so the
// archive contains file entries only. Empty copy options make both engines
// honor the header owner and mode; CopyUIDGID would chown to the container
// user instead.
func materializeConfigs(ctx context.Context, cli dockerclient.DockerClient, containerID, serverName string, configs []runtime.ConfigFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	archive, err := configArchive(ctx, serverName, configs)
	if err != nil {
		return err
	}
	if err := cli.CopyToContainer(ctx, containerID, "/", bytes.NewReader(archive), container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("materializing configs for %s: %w", serverName, err)
	}
	return nil
}

func configArchive(ctx context.Context, serverName string, configs []runtime.ConfigFile) ([]byte, error) {
	type entry struct {
		name string
		mode int64
		uid  int
		gid  int
		body []byte
	}
	entries := make([]entry, 0, len(configs))
	for i, cfg := range configs {
		mode, uid, gid, body, err := config.ReadConfigEntry(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("materializing config %d for %s: %w", i, serverName, err)
		}
		name := strings.TrimPrefix(cfg.Target, "/")
		if name == "" || strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("materializing config %d for %s: target must be an absolute path", i, serverName)
		}
		entries = append(entries, entry{name: name, mode: mode, uid: uid, gid: gid, body: body})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	epoch := time.Unix(0, 0).UTC()
	for _, item := range entries {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     item.name,
			Mode:     item.mode,
			Uid:      item.uid,
			Gid:      item.gid,
			Size:     int64(len(item.body)),
			ModTime:  epoch,
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("materializing configs for %s: %w", serverName, err)
		}
		if _, err := tw.Write(item.body); err != nil {
			return nil, fmt.Errorf("materializing configs for %s: %w", serverName, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("materializing configs for %s: %w", serverName, err)
	}
	return buf.Bytes(), nil
}

// configServerName prefers the logical server label over the replica workload name.
func configServerName(cfg runtime.WorkloadConfig) string {
	if cfg.Labels != nil && cfg.Labels[LabelMCPServer] != "" {
		return cfg.Labels[LabelMCPServer]
	}
	return cfg.Name
}

func configsNeedRecheck(cfg runtime.WorkloadConfig) bool {
	if len(cfg.Configs) > 0 {
		return true
	}
	return cfg.Labels != nil && cfg.Labels[LabelConfigsRevision] != ""
}
