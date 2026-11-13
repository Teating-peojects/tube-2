# storage/ — Phase 5 (Video Upload & Storage) work area

Everything produced for `xhamtube_phase_5_video_upload_storage.md` lives in this folder:
audit, plan, progress tracking, baseline evidence, and (in later phases) the OpenAPI file,
draft migrations and test assets.

```
storage/
  README.md                         this file
  PROGRESS.md                       phase-by-phase status (updated at the end of every phase)
  REMAINING_TASKS.md                open items, ordered
  docs/
    PHASE_5_AUDIT.md                requirement-by-requirement status, verified against code and real runs
    PHASE_5_IMPLEMENTATION_PLAN.md  phases A-G, route/naming mapping, decisions
  baseline/                         raw output of the baseline runs (Phase A) + how to reproduce them
```

## Rules for this work (from `instruction.txt` and the phase document)
- Verify code instead of trusting docs; do not rewrite working modules; keep existing API/DB/frontend contracts.
- Mark something verified only when a test or a real run proved it. Label everything COMPLETE /
  PARTIALLY COMPLETE / BLOCKED / NOT IMPLEMENTED.
- Never claim production readiness without verification.

## Open decision: where does new Go code live?
Go's `internal/` rule means code that must compile into the API (`github.com/redxtube/api`) has to sit
under `backend/`. A separate module in `storage/` could not import `backend/internal/...`.
Proposed split: **all documents, plans, OpenAPI, migration drafts, scripts, test fixtures and patches live
here**; the few source edits that must compile into the API/web are made in `backend/` and `web/` and are
listed per phase in `PROGRESS.md`. Nothing outside `storage/` has been modified so far.
