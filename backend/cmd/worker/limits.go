package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redxtube/api/internal/worker"
)

// loadLimits reads the Phase 6 limits (what an upload may be) and the encoding profile (how it is
// encoded) from the environment. An unset or empty variable keeps the built-in default, which is how
// docker compose passes variables that were not configured. Values the database or the pipeline cannot
// honour are refused here, at startup, never discovered on the first upload.
func loadLimits(getenv func(string) string) (worker.Policy, worker.Profile, time.Duration, error) {
	policy, profile := worker.DefaultPolicy(), worker.DefaultProfile()
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }

	for _, o := range []struct {
		key string
		set func(int)
	}{
		{"WORKER_MAX_DURATION_SECONDS", func(n int) { policy.MaxDurationSec = n }},
		{"WORKER_MAX_SHORT_SIDE", func(n int) { policy.MaxShortSide = n }},
		{"WORKER_MAX_LONG_SIDE", func(n int) { policy.MaxLongSide = n }},
		{"WORKER_MAX_FPS", func(n int) { policy.MaxFPS = float64(n) }},
		{"WORKER_MAX_STREAMS", func(n int) { policy.MaxStreams = n }},
		{"WORKER_MIN_FREE_MB", func(n int) { policy.MinFreeBytes = uint64(n) << 20 }},
		{"WORKER_SEGMENT_SECONDS", func(n int) { profile.SegmentSeconds = n }},
		{"WORKER_AUDIO_KBPS", func(n int) { profile.AudioKbps = n }},
		{"WORKER_THUMBNAILS", func(n int) { profile.ThumbCount = n }},
	} {
		if v := get(o.key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return policy, profile, 0, fmt.Errorf("%s must be a non-negative integer (got %q)", o.key, v)
			}
			o.set(n)
		}
	}
	if v := get("FFMPEG_PRESET"); v != "" {
		profile.Preset = v
	}
	if v := get("WORKER_PROFILE_VERSION"); v != "" {
		profile.Version = v
	}
	if v := get("WORKER_LADDER"); v != "" {
		rungs, err := worker.ParseLadder(v)
		if err != nil {
			return policy, profile, 0, fmt.Errorf("WORKER_LADDER: %w", err)
		}
		profile.Rungs = rungs
		if get("WORKER_PROFILE_VERSION") == "" {
			return policy, profile, 0, errors.New("WORKER_LADDER changes the encoding recipe: also set WORKER_PROFILE_VERSION (it is stored on every video, so the change stays traceable)")
		}
	}
	if err := policy.Check(); err != nil {
		return policy, profile, 0, fmt.Errorf("worker limits: %w", err)
	}
	if err := profile.Check(); err != nil {
		return policy, profile, 0, fmt.Errorf("encoding profile: %w", err)
	}
	retention := 24 * time.Hour
	if v := get("WORKER_FAILED_OUTPUT_RETENTION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return policy, profile, 0, fmt.Errorf("WORKER_FAILED_OUTPUT_RETENTION must be a duration like 24h or 90m (got %q)", v)
		}
		retention = d
	}
	return policy, profile, retention, nil
}
