# [Date] - [Plan Name]

**Status**: Draft | In Progress | Complete  
**Started**: [Date]  
**Owner**: [Your Name]

---

## Background

[Why this change is needed. What problem does it solve? What's the current state?]

**Related**: [Links to related issues, docs, PRs, or previous plans]

---

## Goals

- [ ] Goal 1: Specific, measurable outcome
- [ ] Goal 2: What the user/system will be able to do
- [ ] Goal 3: What metrics/criteria define success?

---

## Approach

[High-level overview of the solution. What's the strategy?]

### Architecture Changes

[If applicable, describe any structural changes to the system]

```
Diagram or pseudocode showing the new flow
```

### Data Model Changes

[If applicable, describe schema changes, new tables, data migrations]

---

## Implementation Steps

### Phase 1: [Phase Name]

- [ ] Step 1: [Description + file/location]
- [ ] Step 2: [Description + file/location]
- [ ] Step 3: [Description + file/location]

**Commits**:
(Will fill as you progress)

### Phase 2: [Phase Name]

- [ ] Step 1: [Description]
- [ ] Step 2: [Description]

**Commits**:
(Will fill as you progress)

---

## Testing Strategy

- [ ] Unit tests: Where? What coverage?
- [ ] Integration tests: Test what flows?
- [ ] Manual testing: What user scenarios to verify?
- [ ] Backward compatibility: Will this break existing users?

---

## Rollout Plan

[How will this be deployed? Are there risks? Gradual rollout?]

- [ ] Stage 1: Dev/test environment
- [ ] Stage 2: Staging/beta environment
- [ ] Stage 3: Production rollout

---

## Timeline

| Task | Start | End | Duration |
|------|-------|-----|----------|
| Phase 1 | [date] | [date] | [X days] |
| Phase 2 | [date] | [date] | [X days] |
| Testing | [date] | [date] | [X days] |
| **Total** | | | **X days** |

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| [Risk description] | High/Medium/Low | [How to mitigate] |
| [Risk description] | High/Medium/Low | [How to mitigate] |

---

## Acceptance Criteria

- [ ] All unit tests pass
- [ ] All integration tests pass
- [ ] Manual testing complete and verified
- [ ] Code review approved
- [ ] CHANGELOG.md updated
- [ ] Documentation updated (if applicable)
- [ ] VERSION bumped (if releasing)
- [ ] No regressions in existing features

---

## Completed Work

### Phase 1 Progress

| Commit | Message | Date |
|--------|---------|------|
| `abc1234` | Step 1 complete | 2026-04-10 |
| `def5678` | Step 2 complete | 2026-04-11 |
| `ghi9012` | Step 3 complete | 2026-04-12 |

### Phase 2 Progress

(To be filled as work progresses)

---

## Notes & Decisions

[Track any decisions made, trade-offs considered, or design notes]

- **Decision**: Why did we choose approach X over Y?
- **Trade-off**: We gain Z but lose feature W
- **Learnings**: What did we discover during implementation?

---

## References

- [Link to related PR/issue]
- [Link to AGENTS.md or specialist guide]
- [Link to related code]

---

## Sign-Off

- [ ] Implementation complete
- [ ] Testing verified
- [ ] Code review approved by: [Name]
- [ ] Ready for merge/release

---

**Template Version**: 1.0 | **Last Updated**: 2026-04-10

---

## How to Use This Template

1. **Copy this file**: `cp TEMPLATE.md 20260410-01\ -\ Your\ Plan\ Name.md`
2. **Customize**: Fill in your plan details
3. **As you work**: Update status, check off steps, add commit hashes
4. **When complete**: Mark `Status: Complete` and ensure sign-off section is filled

**Naming Convention**: `YYYYMMDD-NN - [Category] [Brief Description].md`

- `YYYYMMDD`: Date you start the plan
- `NN`: Daily sequence (01, 02, 03...) if multiple plans same day
- Example: `20260410-01 - Add streaming response cache.md`

---
