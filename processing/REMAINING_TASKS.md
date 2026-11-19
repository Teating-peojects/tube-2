# Remaining tasks

1. (done) Owner decisions D1, D2, D3, D8: all defaults.
2. (done) B6.2 job lifecycle. Open follow-ups from it: heartbeat-error path untested; worker Dockerfile/compose service; Redis `redx:rt` ping at finalize.
3. B6.3 inspection: ffprobe JSON, validation + safe error codes, `short_too_large` rule, METADATA job enqueues the single HLS job (D3), extend status endpoint (B6.7).
4. B6.4 transcode/HLS (MPEG-TS), B6.5 storage upload + thumbnails + `freeStorage` prefix `videos/`, B6.6 finalize (hash, moderation re-check, READY, variants swap in one transaction), B6.7 API/status + frontend states, B6.8 report.
5. B7 - B10 as in `PROMPT_B6_TO_B10.md`.
6. Revoke the GitHub token when work is finished.
