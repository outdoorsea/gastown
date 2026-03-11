# Technical Feasibility

## Summary

The Convoy Stage & Launch PRD is **highly feasible** — and in fact, the
implementation is already 95%+ complete in the codebase. `convoy_stage.go`
(2,101 LOC), `convoy_launch.go` (332 LOC), and comprehensive tests (3,556 LOC)
already exist. Commands are registered in the CLI, staged status constants are
defined, daemon guards are in place, and DAG/wave computation algorithms are
implemented and tested.

The PRD is technically sound. The hardest problems (cycle detection, wave
computation, daemon guard for staged convoys, status transition validation) are
all solved in the existing code. The one architecturally-blocking open question
(Q1: status format) has been resolved in favor of underscores (`staged_ready`,
`staged_warnings`), which pass the `bd doctor` regex `^[a-z][a-z0-9_]*$`.

## Findings

### Critical Gaps / Questions

1. **The PRD describes work that is already implemented.**
   - `convoy_stage.go` (2,101 lines), `convoy_launch.go` (332 lines) exist and
     are registered as subcommands. `gt convoy stage --help` returns valid output.
   - Status constants `staged_ready` and `staged_warnings` are defined in
     `convoy.go:92-93`.
   - `ensureKnownConvoyStatus` (line 100) already accepts all 4 statuses.
   - `validateConvoyStatusTransition` (line 121) handles staged->open, staged->closed,
     and staged<->staged transitions.
   - Daemon guard `isConvoyStaged` exists in `operations.go:114-122` with fail-open
     semantics.
   - DAG construction, DFS cycle detection (3-color marking), and Kahn's topological
     sort for wave computation are all implemented in `convoy_stage.go`.
   - **Why this matters:** The PRD reads as a greenfield design doc, but the
     implementation is nearly complete. The question is whether the *existing*
     implementation matches the PRD spec, not whether the spec is buildable.
   - **Suggested question:** Has the existing implementation been validated against
     this PRD? Is this PRD retroactive documentation, or was it written before
     implementation? If before, has a gap analysis been done?

2. **Open Question Q1 is resolved in code but still marked OPEN in the PRD.**
   - The PRD lists Q1 (status format: colons vs underscores vs hyphens) as
     architecturally blocking, but the code uses underscores (`staged_ready`,
     `staged_warnings`) which satisfy the `bd doctor` regex.
   - **Why this matters:** If Q1 is truly open, someone might change the format
     later and invalidate the existing 102 references to `staged_ready` across
     the codebase.
   - **Suggested question:** Can Q1 be marked RESOLVED with "underscores" as the
     answer, since the code already implements this?

### Important Considerations

1. **`bd doctor` compatibility is handled but not explicitly tested.**
   - The code uses underscores in status names, which pass the regex
     `^[a-z][a-z0-9_]*$`. However, the PRD notes that `bd doctor` rejects
     colons — this implies the `bd doctor` validation has been considered.
   - No test explicitly validates that `bd doctor` passes on a convoy with
     `staged_ready` status. If `bd doctor` has additional validation beyond the
     regex (e.g., an allowlist of known statuses), staged convoys might trigger
     warnings.
   - **Recommendation:** Add a test or manual verification that `bd doctor`
     doesn't warn on `staged_ready` / `staged_warnings` statuses.

2. **The event-driven daemon path uses fail-open semantics for staged guard.**
   - `isConvoyStaged` (operations.go:117-122) returns `false` on store errors,
     meaning a Dolt outage during a close event would cause the daemon to attempt
     feeding a staged convoy. This is consistent with the codebase's existing
     fail-open pattern for `isIssueBlocked`, but worth noting.
   - **Impact:** Low. A Dolt outage would cause many other failures first. The
     daemon would attempt to feed but the tasks themselves would likely fail to
     dispatch.

3. **Wave computation is informational only (by design, but has implications).**
   - The PRD correctly notes that waves are computed at stage time but runtime
     dispatch uses the daemon's per-cycle `isIssueBlocked` checks. This means
     the staged wave plan can diverge from actual execution order if dependencies
     change between staging and launch.
   - This is the correct design (runtime should be authoritative), but users may
     be confused if the displayed wave plan doesn't match execution.
   - **Recommendation:** The launch output or docs should note that the wave plan
     is a snapshot, not a guarantee.

4. **Cross-rig beads in DAG walking.**
   - The PRD mentions "Epic DAG walking may involve cross-rig beads." The code
     uses `beads.ExtractPrefix` + `GetRigNameForPrefix` for rig resolution, which
     works via `routes.jsonl`. However, if a bead prefix has no route entry,
     `GetRigNameForPrefix` returns empty string — the PRD's US-002 AC-2 catches
     this as an error.
   - **Edge case:** If routes.jsonl is stale or incomplete, valid beads could be
     rejected. The error message should suggest updating routes.

5. **FR-7 dispatch path correctly avoids auto-convoy creation.**
   - The PRD is explicit that launch must use internal Go dispatch functions, not
     `gt sling` CLI, to avoid creating duplicate auto-convoys. The code implements
     this via `dispatchTaskDirect` in `convoy_launch.go`.
   - This is the correct approach and avoids the subtle bug of one convoy per task.

### Observations

1. **Test coverage is comprehensive.** `convoy_stage_test.go` (2,764 lines) and
   `convoy_launch_test.go` (792 lines) cover cycle detection, wave computation,
   orphan detection, status transitions, JSON output, and dispatch failures. This
   reduces risk significantly.

2. **The `computeTiers` function in `molecule_dag.go` (prior art) silently breaks
   on cycle detection.** The PRD correctly identifies this limitation and
   specifies a separate `detectCycles` function that returns the cycle path. The
   new implementation in `convoy_stage.go` uses DFS with 3-color marking and
   returns the full cycle path, which is the right approach.

3. **Re-staging (FR-8) is handled cleanly.** The code updates the existing convoy
   in place rather than creating a duplicate. This is consistent with the PRD
   spec and avoids convoy proliferation.

4. **The stranded scan path is safe by accident** (uses `bd list --status=open`
   which excludes staged convoys). The PRD correctly identifies this but the
   robustness depends on the query behavior. If beads ever adds wildcard or
   prefix matching to `--status`, this guard would break. Consider adding an
   explicit comment in the stranded scan code noting this assumption.

5. **No `--force` flag on stage command (only on launch).** The PRD is clear that
   `--force` is a launch-time decision, not a stage-time one. Staging with
   warnings always creates a `staged_warnings` convoy. This is correct.

6. **Mixed input detection (FR-10)** prevents confusing edge cases like
   `gt convoy stage <epic-id> <task-id>`. This is a good ergonomic guard.

## Confidence Assessment

**High** — This PRD is technically feasible with no blocking issues. The
implementation already exists and appears to match the spec closely. The hardest
problems (graph algorithms, daemon guards, status transitions) are solved. The
remaining work is verification that the existing code matches all PRD acceptance
criteria, and resolution of the still-open Q1.

The only risk is if the existing implementation has subtle divergences from the
PRD spec that haven't been caught — but the comprehensive test suite (3,556 LOC)
mitigates this significantly.
