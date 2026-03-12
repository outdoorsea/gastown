# Scope Analysis

## Summary

The PRD for Convoy Stage & Launch defines a well-bounded feature set with clear in/out boundaries. The Non-Goals section (Section 7) explicitly defers several adjacent features (sub-epic status management, auto-formula detection, coordinator polecat, `--infer-blocks`), which is strong scope discipline. The core scope — pre-flight validation, wave computation, staged convoy lifecycle, and Wave 1 dispatch — forms a coherent unit that addresses real operational pain points.

However, there are several areas where scope creep risk is elevated: the staged status question (Q1) has cascading impact across the codebase that could expand the PR surface significantly, the `--json` output commitment creates an implicit stability contract, and several user stories contain acceptance criteria that blur the line between "this PR" and "future work." The PRD also touches daemon behavior (feeding guards) which broadens blast radius beyond the `stage`/`launch` commands themselves. Overall, the scope is well-defined but the implementation surface is larger than the user story count suggests.

## Findings

### Critical Gaps / Questions

1. **The staged status format question (Q1) is architecturally blocking but unresolved**
   - The PRD itself acknowledges this is "architecturally blocking" — the daemon guard, FR-6, and most integration tests depend on the answer. Yet it remains OPEN.
   - **Why this matters:** Without resolving Q1, implementation cannot proceed in a stable direction. If `staged_ready` triggers `bd doctor` warnings, that's a persistent friction point. If the format changes mid-implementation, it could require rewriting tests and daemon guards.
   - **Suggested question:** What is the decided status format — underscores (`staged_ready`), hyphens (`staged-ready`), or some other convention? Is `bd doctor`'s regex being updated to accommodate this, or are the warnings acceptable?

2. **Daemon modification scope is understated**
   - The PRD identifies two daemon paths needing guards (event-driven in `operations.go` and stranded scan in `convoy.go`), but the actual codebase shows the convoy manager (`convoy_manager.go`) has additional touchpoints: `pollStore` processes close events and calls `CheckConvoysForIssue`, the stranded scan runs `gt convoy stranded --json`, and `feedFirstReady` dispatches directly.
   - **Why this matters:** The PRD scopes daemon changes as "add a guard," but ensuring staged convoys are invisible to the daemon across all code paths is a cross-cutting concern, not a localized change. Missing one path means staged convoys could be prematurely fed.
   - **Suggested question:** Has a comprehensive audit been done of all daemon code paths that could interact with staged convoys? The "stranded scan is safe by accident" note is fragile — should there be an explicit guard there too?

3. **Mixed input types (FR-10) adds significant validation complexity**
   - FR-10 requires detecting and rejecting mixed input types (epic + task IDs in same invocation). This implies the command must first resolve each input's type before deciding whether the combination is valid.
   - **Why this matters:** This is scope that wasn't in any user story — it's a functional requirement without acceptance criteria. It adds validation code, error messages, and test cases that aren't tracked in the user story structure.
   - **Suggested question:** Is FR-10 essential for v1, or could the command simply treat all inputs uniformly (resolve each, build the combined DAG)? What actual failure mode does mixed-input detection prevent?

### Important Considerations

1. **`--json` output creates an implicit API contract**
   - US-011 says the JSON output should be "stable enough for design-to-beads to depend on, but mark it as experimental in v1." This is contradictory — either it's stable enough to depend on (requiring backwards compatibility discipline) or it's experimental (subject to breaking changes).
   - The design-to-beads pipeline is listed as the primary consumer, which means breaking JSON changes will break an active workflow even if marked "experimental."
   - **Recommendation:** Either commit to the JSON schema as stable from day one (with versioning), or explicitly document that design-to-beads must pin to a specific gastown version and tolerate breakage.

2. **Re-staging scope (FR-8, US-007 AC-7) is deceptively complex**
   - "Re-staging an existing convoy-id re-analyzes and updates the status" implies: finding the existing convoy, loading its tracked beads, re-running the full analysis pipeline, updating status, and handling the case where beads have been added/removed since the original staging.
   - This is essentially a full `stage` run with update semantics instead of create semantics, plus conflict detection (what if tracked beads changed status between stage and re-stage?).
   - **Recommendation:** Consider whether re-staging is truly v1 or could be deferred. The primary workflow (stage → launch) doesn't require it. Re-staging is an error-recovery/iteration path that adds significant complexity.

3. **Cross-rig bead handling is mentioned but not fully scoped**
   - The PRD mentions "Epic DAG walking may involve cross-rig beads" in Technical Considerations but doesn't specify how cross-rig beads affect wave computation, dispatch, or error reporting.
   - The existing `fetchCrossRigBeadStatus` in `operations.go` shows this is already a known complexity point with fallback behavior.
   - **Recommendation:** Explicitly scope whether cross-rig beads are in or out for v1. If in, add acceptance criteria for cross-rig behavior in US-001 (DAG construction) and US-002 (error detection).

4. **Orphan detection scope varies by input type (US-003 AC-1)**
   - "For epic input only... For task-list input, isolation is expected" — this input-type-dependent behavior adds branching logic to the warning system. It's a reasonable design choice but increases test matrix.
   - **Recommendation:** Ensure tests cover both input types' orphan behavior explicitly.

5. **The PRD modifies existing status infrastructure**
   - Adding `staged_ready`/`staged_warnings` to `ensureKnownConvoyStatus` and `validateConvoyStatusTransition` changes shared infrastructure used by 14+ callsites. While the changes are additive (new valid statuses, new valid transitions), any bug here affects all convoy operations.
   - **Recommendation:** The blast radius of status infrastructure changes should be called out as a risk. Targeted tests for all existing transitions should remain passing.

### Observations

1. **Good scope discipline in Non-Goals section.** The explicit deferral of sub-epic status management, auto-formula detection, coordinator polecat, `--infer-blocks`, and capacity plumbing is well-handled. Each deferral is tied to a specific milestone, preventing "while we're in there" creep.

2. **The PRD correctly identifies code reuse opportunities.** References to `beads.ExtractPrefix`, `IsSlingableType`, `isIssueBlocked`, and `computeTiers` as prior art show awareness of existing infrastructure. The note about `createBatchConvoy` needing adaptation (multi-rig title format, staged status) is precisely the kind of detail that prevents implementers from naively copy-pasting.

3. **Wave computation is explicitly informational.** The distinction that "Runtime dispatch uses the daemon's per-cycle `isIssueBlocked` checks" is important scope discipline — it means wave display doesn't need to be perfectly synchronized with runtime behavior, reducing implementation pressure.

4. **`gt convoy launch` as alias (US-010) is mostly scope-neutral but has an edge case.** AC-3 says launching an already-staged convoy skips re-analysis, while AC-1/AC-2 say launching a non-convoy input does full stage+launch. This means `launch` has two distinct code paths, which is worth noting for test coverage.

5. **Natural phase seams exist.** The PRD could be split into two phases:
   - **Phase 1:** Stage command (US-001 through US-007, US-011) — analysis, display, staged convoy creation. No dispatch, no daemon changes.
   - **Phase 2:** Launch command (US-008 through US-010) — dispatch, daemon guards, status transitions.

   This phasing would reduce blast radius per PR and allow the stage command to be used for validation-only workflows while launch is being built. However, since the code already appears to be implemented, this observation is primarily relevant for review/testing strategy.

6. **Quality gates are well-scoped.** The gate command (`go vet && go build && go test ./internal/cmd/... ./internal/convoy/... ./internal/daemon/...`) targets exactly the affected packages without running the full test suite. This is appropriate for the scope.

7. **The MVP is clear.** The smallest version that delivers value is: `gt convoy stage <epic-id>` with DAG display + wave computation + error detection (US-001, US-002, US-004, US-005, US-006). Everything else (warnings, convoy creation, launch, JSON) builds on this core. If tradeoffs arise, the stage-as-analysis-tool is the irreducible core.

## Confidence Assessment

**Medium-High.** The PRD's scope boundaries are well-defined with an explicit Non-Goals section and milestone deferrals. The main risk is the unresolved Q1 (status format) which could expand the implementation surface depending on the answer. The daemon modification scope is slightly understated. The feature set is cohesive and the seams between v1 and future work are clearly drawn. The primary concern is implementation surface area — 11 user stories across 5+ files with daemon interaction is substantial for a single PR, even though each piece is individually well-scoped.
