package runs

import "time"

// Status is value-free recorder health. It is independent of the JSONL
// destination so a broken writer can still report failure.
type Status struct {
	Enabled              bool              `json:"enabled"`
	Effective            bool              `json:"effective"`
	WriterHealth         string            `json:"writer_health"`
	QueueDepth           int               `json:"queue_depth"`
	QueueCapacity        int               `json:"queue_capacity"`
	Drops                map[string]uint64 `json:"drops"`
	Failures             map[string]uint64 `json:"failures"`
	LastSuccessfulAppend *time.Time        `json:"last_successful_append,omitempty"`
	LastSuccessfulSync   *time.Time        `json:"last_successful_sync,omitempty"`
	ProcessStartedAt     time.Time         `json:"process_started_at"`
	HistoricalLoss       string            `json:"historical_loss"`
	WipeEpoch            uint64            `json:"wipe_epoch"`
	Generation           uint64            `json:"generation"`
	RecorderInstanceID   string            `json:"recorder_instance_id"`
	LogicalBytes         int64             `json:"logical_bytes"`
	Synced               bool              `json:"synced"`
	Known                bool              `json:"known"`
	OmitLabels           bool              `json:"omit_labels"`
	RetentionMaxBytes    int64             `json:"retention_max_bytes"`
	RetentionMaxAgeDays  int               `json:"retention_max_age_days"`
}

// Config is the runtime recorder configuration.
type Config struct {
	Enabled       bool
	OmitLabels    bool
	StackName     string
	MaxBytes      int64
	MaxAge        time.Duration
	QueueSize     int
	SyncInterval  time.Duration
	ShutdownDrain time.Duration
	SegmentSize   int64
	Dir           string
}

const (
	DefaultQueueSize      = 1024
	DefaultMaxRecordBytes = 8192
	DefaultSyncInterval   = 2 * time.Second
	DefaultShutdownDrain  = 2 * time.Second
	DefaultMaxBytes       = 100 << 20
	DefaultMaxAge         = 7 * 24 * time.Hour
	pruneInterval         = time.Hour
)
