package main

import (
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/worker"
)

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestUnsetAndEmptyVariablesKeepTheDefaults(t *testing.T) {
	// docker compose passes unconfigured variables as empty strings
	empty := map[string]string{}
	for _, k := range []string{"WORKER_MAX_DURATION_SECONDS", "WORKER_MAX_SHORT_SIDE", "WORKER_MAX_LONG_SIDE", "WORKER_MAX_FPS", "WORKER_MAX_STREAMS",
		"WORKER_MIN_FREE_MB", "FFMPEG_PRESET", "WORKER_SEGMENT_SECONDS", "WORKER_AUDIO_KBPS", "WORKER_THUMBNAILS", "WORKER_LADDER",
		"WORKER_PROFILE_VERSION", "WORKER_FAILED_OUTPUT_RETENTION"} {
		empty[k] = ""
	}
	for name, env := range map[string]func(string) string{"nil map": envMap(nil), "empty strings": envMap(empty), "blanks": envMap(map[string]string{"WORKER_MAX_FPS": "   "})} {
		po, pr, ret, err := loadLimits(env)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if po.MaxDurationSec != 3600 || po.MaxFPS != 120 || pr.Version != "h264-aac-hls-v1" || pr.Preset != "veryfast" || ret != 24*time.Hour {
			t.Errorf("%s: defaults changed: %+v %+v %v", name, po, pr, ret)
		}
	}
}

func TestConfiguredValuesAreApplied(t *testing.T) {
	po, pr, ret, err := loadLimits(envMap(map[string]string{
		"WORKER_MAX_DURATION_SECONDS": "1200", "WORKER_MAX_SHORT_SIDE": "720", "WORKER_MAX_LONG_SIDE": "1280", "WORKER_MAX_FPS": "30",
		"WORKER_MAX_STREAMS": "4", "WORKER_MIN_FREE_MB": "2048", "FFMPEG_PRESET": "fast", "WORKER_SEGMENT_SECONDS": "4",
		"WORKER_THUMBNAILS": "3", "WORKER_LADDER": "360:700,720:2500", "WORKER_PROFILE_VERSION": "custom-v2", "WORKER_FAILED_OUTPUT_RETENTION": "90m",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if po.MaxDurationSec != 1200 || po.MaxShortSide != 720 || po.MaxLongSide != 1280 || po.MaxFPS != 30 || po.MaxStreams != 4 || po.MinFreeBytes != 2048<<20 {
		t.Errorf("limits: %+v", po)
	}
	if pr.Preset != "fast" || pr.SegmentSeconds != 4 || pr.ThumbCount != 3 || pr.Version != "custom-v2" || len(pr.Rungs) != 2 || ret != 90*time.Minute {
		t.Errorf("profile: %+v %v", pr, ret)
	}
	if got := pr.Plan(1280, 720); len(got) != 2 {
		t.Errorf("ladder not used: %+v", got)
	}
	_ = worker.DefaultPolicy
}

func TestBadConfigurationFailsAtStartup(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"not a number":                {map[string]string{"WORKER_MAX_FPS": "fast"}, "WORKER_MAX_FPS"},
		"negative":                    {map[string]string{"WORKER_MAX_STREAMS": "-1"}, "WORKER_MAX_STREAMS"},
		"raising past the database":   {map[string]string{"WORKER_MAX_SHORT_SIDE": "2160"}, "database limit"},
		"long side past the database": {map[string]string{"WORKER_MAX_LONG_SIDE": "3840"}, "database limit"},
		"unknown preset":              {map[string]string{"FFMPEG_PRESET": "placebo"}, "preset"},
		"absurd segments":             {map[string]string{"WORKER_SEGMENT_SECONDS": "60"}, "segment"},
		"ladder without a version":    {map[string]string{"WORKER_LADDER": "360:700,720:2500"}, "WORKER_PROFILE_VERSION"},
		"ladder rung not in the db":   {map[string]string{"WORKER_LADDER": "540:900", "WORKER_PROFILE_VERSION": "v2"}, "540p"},
		"malformed ladder":            {map[string]string{"WORKER_LADDER": "720", "WORKER_PROFILE_VERSION": "v2"}, "WORKER_LADDER"},
		"bad retention":               {map[string]string{"WORKER_FAILED_OUTPUT_RETENTION": "tomorrow"}, "WORKER_FAILED_OUTPUT_RETENTION"},
		"zero retention":              {map[string]string{"WORKER_FAILED_OUTPUT_RETENTION": "0s"}, "WORKER_FAILED_OUTPUT_RETENTION"},
	}
	for name, c := range cases {
		if _, _, _, err := loadLimits(envMap(c.env)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error mentioning %q", name, err, c.want)
		}
	}
}
