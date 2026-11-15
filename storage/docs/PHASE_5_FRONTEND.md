# Phase 5 / E — upload frontend

Scope: verify the upload UI against the Phase 5 document, fill the gaps, wire the backend retry endpoint (Phase C).
Files: `web/src/lib/uploader.ts`, `features/upload/{useUpload.ts,UploadProgress.tsx,UploadView.tsx}`, `services/upload.ts`,
`services/mock/studio.ts`, `config/endpoints.ts`, `types/index.ts` (+ tests).

## What existed, what was missing
| Requirement | Before | Now |
|---|---|---|
| Drop zone, format/size validation | existed (`.avi` already removed in Phase B) | unchanged |
| Progress, speed, ETA | existed | unchanged (hidden while offline) |
| Cancel | existed | **fixed race:** cancelling while the session is being created left that session open on the server and flipped the UI back to "uploading". Now the late session is closed and the UI stays cancelled (also during recovery) |
| Retry / resume of finished parts | existed | unchanged |
| Network loss | 4 attempts (about 7 s of backoff), then a failed state | pauses while the browser is offline (no attempts used), resumes by itself on `online`; a failure that already happened resumes by itself when the connection returns; a connection with no progress for 60 s is aborted and retried (a silent drop never fired an error) |
| Expired presigned URL / session | 4 pointless retries per part, then a new session was started without closing the old one (it stayed open, counting toward the open-upload cap) | a 4xx is not retried; one automatic recovery through `POST /upload/:id/retry` (same ids, new URLs); fallback closes the old session first, then starts a new one; a transient failure keeps the old session |
| Server refusals | one generic message | per-code messages; `invalid_file` / `validation_failed` / too large offer "Choose another file"; `size_mismatch`, `upload_closed`, `parts_mismatch`, `limit_reached`, 429, 401/403, 502/503 each say what to do. The server's own text is never shown |
| Leaving the page | `beforeunload` with `preventDefault()` only | also sets `returnValue` (needed by some browsers) |
| Mobile | stacked layout, 40 px buttons | action buttons are 44 px tall; the progress panel already sits above the form on small screens |
| Reset after a finished upload | would have sent `DELETE` for a published upload (the server refuses it) | resets without calling the API unless the upload never finished |

## Verification
Vitest 105/105 (was 64): `uploader` 23, `useUpload` 20 (new), `UploadProgress` 8, rest unchanged; `tsc` clean; `next lint` clean
(the same two pre-existing warnings). Mutation checks: breaking any of the cancel race fix, the non-retry of 4xx, the
offline attempt accounting, the stall watchdog re-arming, the recovery guard or the reset guard makes a test fail.

**Not run:** Playwright e2e (no browsers in the sandbox, and a spec that cannot be run was not added), `next build`
(Google Fonts unreachable), a real browser against a real API and bucket. No e2e was changed; the existing upload spec
exercises the mock flow, which this change does not alter.

## Limits
- A page reload or closed tab cannot resume an upload: the browser cannot restore the file handle. The user picks the
  file again; the abandoned session expires and the backend cleans it up (Phases C/D).
- The `retryable` flag of `GET /upload/:id/status` is typed but unused: the app has no list of past uploads. It is meant for a Studio "your upload expired, retry" view.
- A reopened upload re-sends every part (the old multipart upload is gone), so a 5 GiB upload restarts from zero.
