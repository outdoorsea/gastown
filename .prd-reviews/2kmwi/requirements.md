# Requirements Completeness

## Summary

The PRD for `gt convoy stage` and `gt convoy launch` is **well-structured and unusually thorough** for a feature PRD. It includes 11 user stories with acceptance criteria, 10 functional requirements, explicit non-goals, technical considerations with codebase references, and open questions. Notably, the codebase already contains substantial implementation of the described features — status constants, validation transitions, DAG construction, wave computation, cycle detection, and dispatch logic all exist in `convoy_stage.go`, `convoy_launch.go`, and related files. This means the PRD is partially retroactive documentation of built functionality, which affects how completeness should be evaluated.

However, several critical gaps exist in **success criteria specificity**, **error recovery definitions**, **failure mode enumeration**, and **non-functional requirements**. A QA engineer would struggle to write integration tests for certain user stories because acceptance criteria describe *what* but not *how much* or *how to verify*. The biggest risk is that the PRD does not define what "correct" looks like for edge cases that span multiple user stories.

## Findings

### Critical Gaps / Questions

**1. No acceptance criteria for the "re-staging" workflow (US-007 AC-7, FR-8)**
- US-007 says "Re-staging an existing convoy-id re-analyzes and updates the status" but does not define:
  - What happens to tracked beads that were added/removed between stagings?
  - Does re-staging preserve existing `tracks` deps or rebuild them from scratch?
  - If a re-staged convoy was `staged_ready` and new analysis finds warnings, what's the transition path?
  - Can a re-stage happen while Wave 1 tasks are already dispatched (if convoy was launched then somehow needs re-staging)?
- **Why this matters:** Re-staging is the most complex state transition in the system. Without explicit acceptance criteria, two engineers would implement it differently.
- **Suggested question:** "When re-staging a convoy, should the tracked bead set be rebuilt from the current epic descendant tree, or preserved from the original staging? What if beads were added/removed from the epic between stagings?"

**2. No definition of "done" for the overall feature — success metrics are vague**
- The "Success Metrics" section lists 6 bullets that are qualitative ("correctly identifies", "catches all", "only sends"). None are measurable or verifiable.
- There is no definition of what test coverage is expected, what manual verification steps confirm the feature works, or what the acceptance test for the round-trip workflow looks like.
- **Why this matters:** Without measurable success criteria, there's no objective way to determine if the implementation is complete. The quality gates section provides `go vet && go build && go test` commands but doesn't specify expected pass counts or coverage thresholds.
- **Suggested question:** "What are the minimum acceptance tests that must pass before this can be considered done? Is there a specific round-trip test scenario (e.g., create epic with 5 tasks, 2 deps, stage, verify waves, launch, verify dispatch) that should be automated?"

**3. Open Question 1 is architecturally blocking but unresolved**
- Q1 asks whether `staged_ready`/`staged_warnings` should be proper beads statuses. The PRD says this is "architecturally blocking" for daemon guards, FR-6, and integration tests.
- The codebase already uses `staged_ready` and `staged_warnings` as string constants, and `isStagedStatus()` checks for `"staged_"` prefix. This suggests the decision was made (underscore format) but Q1 is still marked OPEN.
- **Why this matters:** If Q1 is truly unresolved, most implementation work cannot be finalized. If it's already resolved (the code says yes), Q1 should be marked RESOLVED in the PRD.
- **Suggested question:** "Q1 appears resolved in practice — the codebase uses `staged_ready`/`staged_warnings` with underscore format. Should Q1 be updated to RESOLVED, or is there still uncertainty about `bd doctor` compatibility?"

**4. No error recovery or partial failure handling specification**
- US-008 AC-5 says "If a Wave 1 dispatch fails, continues to next task" but doesn't specify:
  - What status does the failed task get? Does it stay `open`? Get marked `blocked`?
  - What status does the convoy get if *all* Wave 1 dispatches fail?
  - Is there a retry mechanism, or does the user need to re-launch?
  - What console output does the user see for partial failures?
- **Why this matters:** Partial dispatch failure is a production scenario. The user needs to know what happened and what to do about it. Without defined behavior, the implementation will invent its own error handling.
- **Suggested question:** "When Wave 1 dispatch partially fails (e.g., 3 of 5 tasks dispatched, 2 failed), what should the convoy status be? What should the user do — re-launch, manually sling failed tasks, or something else?"

**5. No rollback/undo specification**
- There is no way to un-stage or un-launch a convoy described in the PRD.
- `validateConvoyStatusTransition` allows `staged_* → closed` (cancel flow), but no user story or AC describes this workflow.
- **Why this matters:** Users will inevitably stage something wrong and want to cancel it. Without a defined cancellation path, they'll need to manually close the convoy.
- **Suggested question:** "Should there be a `gt convoy cancel <convoy-id>` or similar command? Or is `bd close <convoy-id>` sufficient for cancelling a staged convoy?"

### Important Considerations

**6. No performance requirements or scale expectations**
- The PRD doesn't specify how many beads the DAG walker should handle. Is 10 tasks the expected case? 100? 1000?
- Wave computation uses topological sort which is O(V+E), but DAG building requires fetching each bead. For an epic with 100 descendants, this means 100+ beads SDK calls.
- No timeout is specified for the staging analysis.
- **Recommendation:** Add a note about expected scale (e.g., "designed for convoys up to ~50 tasks; performance for larger convoys is best-effort").

**7. Daemon guard completeness — event-driven vs. stranded scan**
- The PRD's "Technical Considerations" section correctly identifies two daemon paths. The codebase has `isConvoyStaged()` guarding the event-driven path. The stranded scan uses `--status=open` which excludes staged convoys.
- However, the PRD doesn't specify whether the stranded scan should *also* scan staged convoys (e.g., to auto-close abandoned staged convoys that were never launched).
- **Recommendation:** Define a TTL or cleanup policy for staged convoys that are never launched.

**8. `--json` output schema is not specified**
- US-011 says JSON includes specific fields (`errors`, `warnings`, `waves`, etc.) but doesn't provide a JSON schema or example payload.
- The "mark it as experimental in v1" note is good, but consumers (design-to-beads) need *something* to code against.
- **Recommendation:** Include a sample JSON output in the PRD or reference a schema file.

**9. Mixed input detection (FR-10) lacks specific error message format**
- FR-10 says mixed inputs should be "detected and rejected with a clear error message" but doesn't define what "mixed" means precisely. Is an epic ID + a task that's a child of that epic mixed? Or only epic + unrelated task?
- **Recommendation:** Define the detection heuristic: "If args contain both epic-type and non-epic-type beads, reject."

**10. No monitoring, alerting, or observability requirements**
- The PRD doesn't specify logging expectations for stage/launch operations.
- No metrics are defined (e.g., "convoy staged" counter, "wave dispatch latency" histogram).
- No audit trail: who staged what, when, with what analysis results.
- **Recommendation:** At minimum, define structured log events for stage and launch operations.

### Observations

**11. PRD and implementation are out of sync**
- The PRD describes features as if they need to be built, but the codebase contains substantial implementation. This creates ambiguity: is the PRD a spec for new work, or documentation of completed work? The formula dispatched this as active work with implementation steps, suggesting there's still work to do.
- **Observation:** Clarify the PRD's role — is it guiding new implementation, documenting existing implementation for review, or specifying remaining gaps?

**12. US-005 (DAG tree display) acceptance criteria are UI-specific but untestable**
- "Sub-epics are visually distinct from leaf tasks" and "Blocked tasks show their blocker(s) inline" are visual requirements that can't be verified by `go test`.
- **Observation:** Consider adding golden-file tests for tree output format.

**13. Implicit requirement: `gt convoy status` needs to show wave information**
- The PRD creates staged convoys with wave data but doesn't update `gt convoy status` to display it. Users who stage a convoy and then run `gt convoy status` would expect to see the wave plan.
- **Observation:** This is likely a follow-up requirement.

**14. Quality gates command is specific and testable**
- The quality gates section provides an exact command: `go vet ./... && go build ./... && go test ./internal/cmd/... ./internal/convoy/... ./internal/daemon/... -count=1`. This is good — it's specific, scoped, and reproducible.

## Confidence Assessment

**Medium** — The PRD is above-average in structure and specificity, with acceptance criteria on most user stories and explicit codebase references. However, the completeness score is pulled down by: (1) an unresolved architecturally-blocking open question that appears already resolved in code, (2) missing error recovery and rollback specifications, (3) no measurable success criteria, and (4) no non-functional requirements for performance, monitoring, or observability. A QA engineer could write happy-path tests from this PRD but would need to invent error-path behavior.
