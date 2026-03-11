# Stakeholder Analysis

## Summary

The Convoy Stage & Launch PRD primarily addresses the **human operator** (mayor/overseer) who dispatches work, and the **design-to-beads automation pipeline** that consumes `--json` output. These are well-covered. However, the PRD underspecifies impacts on several downstream systems that will be affected by the introduction of staged statuses and batch Wave 1 dispatch: the daemon's feeding logic, the Dolt data plane under convoy-scale write bursts, the witness agents who must monitor a sudden influx of polecats, and the beads SDK/tooling ecosystem that assumes a simpler status model. The most significant gap is the absence of any discussion of **stale staged convoy cleanup** — staged convoys that are never launched will accumulate indefinitely.

There is a notable tension between the needs of automation consumers (who want deterministic `--json` output and programmatic launch) and human operators (who want rich interactive feedback and the ability to intervene). The PRD handles this reasonably via the `--json` flag split, but the two audiences have subtly different error-handling expectations that aren't addressed.

## Findings

### Critical Gaps / Questions

**1. Stale staged convoy lifecycle — no owner identified**
- The PRD defines how staged convoys are created (US-007) and launched (US-008) but never addresses what happens to a staged convoy that is abandoned. There is no TTL, no cleanup command, no garbage collection, and no status transition to `closed` for unfulfilled staged convoys.
- **Why this matters:** Staged convoys create real beads with `tracks` dependencies. Orphaned staged convoys will accumulate in `bd list`, pollute `gt convoy list` output, and confuse the stranded scan (which currently queries `--status=open` but would need to also exclude staged statuses if query patterns change).
- **Who cares:** Ops/support teams running `bd doctor`, the daemon's stranded scan, anyone reviewing convoy lists.
- **Suggested question:** Who owns cleanup of staged convoys that are never launched? Should there be a TTL (e.g., auto-close after 24h), a `gt convoy discard` command, or manual cleanup via `gt convoy close`?

**2. Dolt write burst from Wave 1 dispatch — no load analysis**
- Launching a convoy dispatches all Wave 1 tasks simultaneously (US-008). Each dispatch creates/updates multiple beads (polecat assignment, status transitions, convoy tracking). For a 15-task Wave 1, this could mean 30-60 Dolt writes in rapid succession.
- **Why this matters:** The CLAUDE.md explicitly warns that Dolt is fragile. The current `gt sling` dispatches one task at a time with natural human pacing. Automated batch dispatch removes that pacing.
- **Who cares:** Every agent in the system — a Dolt hang or crash during convoy launch would block all beads operations system-wide.
- **Suggested question:** Has the Dolt server been load-tested for burst writes? Should Wave 1 dispatch include a brief throttle (e.g., 100ms between dispatches) as a safety valve?

**3. Witness capacity under sudden polecat influx — not mentioned**
- The witness monitors polecat health. Wave 1 dispatch could spawn N polecats simultaneously. The PRD discusses polecat dispatch but never mentions witness scaling.
- **Why this matters:** If the witness has a fixed polling interval and suddenly has 10+ polecats to monitor instead of 2-3, health checks may lag and zombie detection may be delayed.
- **Who cares:** Witness agents, the overseer who depends on witness reliability, and polecats who may become zombies without witness intervention.
- **Suggested question:** Does the witness auto-scale its monitoring based on active polecat count? What is the maximum concurrent polecat count the witness has been tested with?

### Important Considerations

**4. Beads SDK status model expansion — cross-cutting impact**
- Adding `staged_ready` and `staged_warnings` statuses (Open Question Q1) affects every tool that handles bead statuses: `bd doctor`, `bd list`, `bd show`, status validation in the Go SDK, and any external tools that parse bead statuses. The PRD acknowledges Q1 is "architecturally blocking" but doesn't inventory the full blast radius.
- **Who cares:** All beads SDK consumers, `bd` CLI users, any scripts or tools that regex-match on status values, future formula/molecule authors who may want their own custom statuses.
- **Recommendation:** The status format decision should be made with input from the beads maintainer (if different from the convoy author), and the blast radius should be enumerated in a separate design note.

**5. Cross-rig convoy dispatch — rig owner consent not addressed**
- The PRD mentions that stage/launch convoys may span multiple rigs (Technical Considerations). Wave 1 dispatch will spawn polecats in target rigs. The PRD handles rig *availability* (parked/docked checks) but not rig *consent* — can any user dispatch work to any rig?
- **Who cares:** Rig owners/operators who may not expect external convoys to spawn polecats in their rigs. In a multi-team environment, this is a governance question.
- **Recommendation:** Document whether cross-rig dispatch requires any form of rig-owner acknowledgment, or if the existing rig routing infrastructure implies blanket consent.

**6. Conflicting needs: automation speed vs. human review**
- The design-to-beads pipeline wants `gt convoy stage --json | gt convoy launch` as a fast, programmatic pipeline. Human operators want to inspect the staging output, consider warnings, and decide whether to launch. These are fundamentally different interaction models.
- **Who cares:** Both audiences. If the `--json` output is optimized for machines, it may omit context humans need. If the human output is too verbose, automation has to parse noise.
- **Recommendation:** The PRD handles this via the `--json` flag, which is appropriate. Consider whether `--json` output should include a `human_summary` field for logging/audit purposes.

**7. Error recovery after partial Wave 1 dispatch failure**
- US-008 AC-4 says "if a Wave 1 dispatch fails, continues to next task." But the convoy status is already `open` at this point (AC-1). There's no mechanism to retry failed dispatches or to know which Wave 1 tasks failed.
- **Who cares:** The operator who launched the convoy, the support team diagnosing why some tasks ran and others didn't.
- **Recommendation:** Log failed dispatches prominently in console output (US-009 partially covers this). Consider whether the convoy should track dispatch failures as metadata for `gt convoy status` to report.

### Observations

**8. Daemon maintainers — existing guard is fragile**
- The PRD notes the event-driven path in `operations.go` needs an explicit `isStagedConvoy(status)` guard. The stranded scan is "safe by accident" because it queries `--status=open`. This is a correctness concern, not a stakeholder gap, but it affects anyone maintaining the daemon: the "safe by accident" path will break if query patterns change.
- The codebase exploration confirms this: `CheckConvoysForIssue` already has staged-convoy skip logic (DS-07, DS-08 tests), suggesting this has been partially addressed in code but the PRD was written before implementation.

**9. Future TUI consumers (`gt convoy -i`)**
- US-009 AC-4 mentions printing a hint for `gt convoy -i` (interactive TUI monitoring). This sets a user expectation for a feature that doesn't exist yet. If the TUI is far out, the hint may confuse users.
- **Who cares:** Users who see the hint and try to use it.

**10. Support team post-launch needs**
- The PRD doesn't describe what `gt convoy status <convoy-id>` shows for a launched convoy. Support teams diagnosing stalled convoys need: current wave progress, which tasks are blocked, which polecats are alive/dead, and time-since-last-progress. The existing `gt convoy status` likely needs enhancement to be useful for staged/launched convoys.
- **Who cares:** Support/ops teams, the overseer, any human debugging a stuck convoy.

**11. Polecats — transparent stakeholder**
- Polecats themselves are unaffected by stage/launch changes — they receive work the same way regardless of whether it was dispatched via `gt sling` or `gt convoy launch`. The dispatch path is transparent to them. This is a good design property.

## Confidence Assessment

**Medium-High confidence.** The PRD is thorough on the primary workflow (stage → launch → daemon feeds subsequent waves) and correctly identifies the key technical risks (staged status impact, daemon guard paths, cycle detection). The main gaps are in operational concerns: stale convoy cleanup, Dolt load under batch dispatch, witness scaling, and cross-rig governance. These are the kinds of issues that surface in production rather than in design review, which is exactly why this analysis flags them now. The partial implementation already in the codebase (staged convoy skip logic in daemon) suggests some of these concerns have been addressed in code but aren't reflected back into the PRD.
