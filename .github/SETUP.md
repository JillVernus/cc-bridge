# CC-Bridge AI Agent Setup — Complete Guide

**What**: Comprehensive instruction set, skills, and agent configuration for cc-bridge AI-assisted development  
**When**: April 2026  
**Status**: ✅ Ready to use

---

## What Was Created

This document describes all files and configurations added to enable AI agents to develop cc-bridge effectively.

### Directory Structure

```
cc-bridge/
├── .github/copilot-instructions.md          [ENTRY POINT] Main instructions for all agents
├── .github/instructions/                    [SPECIALIZED GUIDES]
│   ├── README.md                            Quick reference & navigation
│   ├── EXAMPLES.md                          13 real-world prompt examples
│   ├── backend.md                           Go development guide (structure, patterns, testing)
│   ├── handler-development.md               HTTP endpoint patterns + streaming + testing
│   ├── provider-integration.md              Adding upstream AI providers
│   ├── frontend-components.md               Vue 3 + Vuetify UI patterns
│   └── database.md                          Schema migrations + SQL patterns
├── .agent.md                                [AGENT CONFIGURATION] Multi-role agent modes
├── .claude/skills/release-workflow/         [AUTOMATION SKILL]
│   └── SKILL.md                             Semantic versioning workflow
└── docs/implementation_plans/
    └── TEMPLATE.md                          [PLAN TEMPLATE] Structured planning format
```

---

## Quick Start: Using These Resources

### For a New Developer

1. **Start here**: Read [.github/copilot-instructions.md](.github/copilot-instructions.md) (5 min)
   - Gives you the full project context
   - Points to specialized guides

2. **Choose your path**: Go to [.github/instructions/README.md](.github/instructions/README.md)
   - "I'm building a handler" → handler-development.md
   - "I'm adding a provider" → provider-integration.md
   - "I'm building UI" → frontend-components.md

3. **See examples**: Check [.github/instructions/EXAMPLES.md](.github/instructions/EXAMPLES.md)
   - 13 real prompts you can adapt
   - Shows how to reference guides effectively

### For AI Agents

**Default behavior** (automatic):
- Loads [.github/copilot-instructions.md](.github/copilot-instructions.md)
- Full project context with links to specialists

**Specialized modes** (opt-in):
```
@claude /backend          # Backend specialist mode
@claude /frontend         # Frontend specialist mode  
@claude /database         # Database specialist mode
@claude /release          # Release specialist mode
```

Each mode loads the appropriate guides and filters context to relevant files.

---

## File Guide

### Entry Points

| File | Purpose | Read Time |
|------|---------|-----------|
| [.github/copilot-instructions.md](.github/copilot-instructions.md) | 🎯 **START HERE** — Main instruction for all agents | 5 min |
| [.github/instructions/README.md](.github/instructions/README.md) | Navigation guide — which file to read for your task | 3 min |
| [.github/instructions/EXAMPLES.md](.github/instructions/EXAMPLES.md) | 13 real prompt examples | 10 min |

### Specialized Guides

| File | For | Read Time |
|------|-----|-----------|
| [.github/instructions/backend.md](.github/instructions/backend.md) | Go development, project structure, patterns | 15-20 min |
| [.github/instructions/handler-development.md](.github/instructions/handler-development.md) | Building HTTP endpoints, streaming, testing | 10-15 min |
| [.github/instructions/provider-integration.md](.github/instructions/provider-integration.md) | Adding upstream providers, protocol conversion | 10-15 min |
| [.github/instructions/frontend-components.md](.github/instructions/frontend-components.md) | Vue 3 components, Vuetify, TypeScript | 10-15 min |
| [.github/instructions/database.md](.github/instructions/database.md) | Schema migrations, SQLite/PostgreSQL | 10-15 min |

### Configurations

| File | Purpose |
|------|---------|
| [.agent.md](.agent.md) | Defines 5 AI agent modes with specialized contexts |
| [.claude/skills/release-workflow/SKILL.md](.claude/skills/release-workflow/SKILL.md) | Automation skill for semantic versioning |

### Templates

| File | Used For |
|------|----------|
| [docs/implementation_plans/TEMPLATE.md](../docs/implementation_plans/TEMPLATE.md) | Planning new features with checkpoints |

---

## Feature Overview

### 1. Main Instructions (.github/copilot-instructions.md)

**What it contains:**
- Project structure & file locations
- Three API pools (Messages, Responses, Gemini)
- Code style guidelines (Go + Vue 3)
- Anti-patterns & rules
- Quick command reference
- Essential API endpoints
- Common development workflows
- Tips for AI assistants

**When to use:**
- New developer orientation
- Quick lookup of patterns
- Understanding architecture

---

### 2. Specialized Guides (.github/instructions/)

#### backend.md — Go Backend Specialist
- Project structure deep-dive
- Handler pattern (gin.HandlerFunc)
- Provider pattern (interface methods)
- Configuration & hot-reload
- Testing strategy (table-driven tests)
- Channel scheduler, metrics, request logging
- Code style (naming, imports, error handling)
- Performance profiling

#### handler-development.md — HTTP Endpoint Pattern
- The handler pattern template
- Request parsing & validation
- Streaming responses (SSE)
- Error handling & HTTP status codes
- Testing with gin.TestMode
- Common tasks (scheduler, metrics, logging)

#### provider-integration.md — Upstream Provider Integration
- Provider interface (3 methods)
- Anatomy of a provider
- Message format conversion patterns
- Stop reason handling
- Token counting
- Streaming format differences
- Registration in factory
- Testing providers

#### frontend-components.md — Vue 3 Component Development
- Composition API pattern (<script setup>)
- Props & emits with TypeScript
- Common patterns (composables, API calls, forms)
- Styling (Vuetify 3 + Tailwind CSS)
- i18n integration
- API client usage
- Pre-commit checklist

#### database.md — Database Migrations
- SQLite / PostgreSQL overview
- Migration file format (UP/DOWN)
- Running migrations (CLI + Makefile)
- Creating new migrations (step-by-step)
- Common patterns (ALTER, rename, data migration)
- Testing migrations
- Troubleshooting
- Database hygiene

---

### 3. Agent Configuration (.agent.md)

**5 Specialized Modes:**

| Mode | Loads | Filters To |
|------|-------|-----------|
| general | Full instructions | All files |
| /backend | backend.md, handler.md, provider.md, database.md | backend-go/** |
| /frontend | frontend-components.md | frontend/** |
| /database | database.md | dbmigrate/** |
| /release | release-workflow/SKILL.md | VERSION, CHANGELOG |

**How to use:**
```
@claude /backend

I need to add a new handler for user quotas.
What's the pattern I should follow?
```

The agent loads backend-focused guide and responds with examples from handler-development.md.

---

### 4. Release Workflow Skill (.claude/skills/release-workflow/)

**Automated semantic versioning:**

```bash
/release-workflow bump minor api
# → bumps 1.2.3 to 1.3.0
# → updates VERSION, frontend/package.json, CHANGELOG.md
# → commits "chore(release): bump version to 1.3.0"
# → tags v1.3.0
```

**Supports:**
- patch, minor, major bumps
- Explicit version specification
- Scope tagging (api, ui, database, etc.)

---

### 5. Implementation Plan Template (docs/implementation_plans/TEMPLATE.md)

**Structured format for feature planning:**

```markdown
# [Date] - [Plan Name]

## Background
Why this change is needed

## Goals
- [ ] Goal 1
- [ ] Goal 2

## Implementation Steps

### Phase 1
- [ ] Step 1: Description
- [ ] Step 2: Description

**Commits**:
(Will fill as you progress)
```

**Naming convention:**
```
20260410-01 - Add quota management.md
↑ date     ↑ sequence, scopes
```

---

## Example Usage Patterns

### Pattern 1: "I'm stuck on handler implementation"

```
What I have: Started building a /api/quota-status endpoint

@claude /backend

I need to [requirement]. 

Do I follow the pattern in handler-development.md?
Is my error handling correct?
```

**Agent response:**
1. Jumps to handler-development.md #The-Handler-Pattern
2. Reviews code against documented patterns
3. Points out issues with examples
4. References testing section for test structure

---

### Pattern 2: "Setting up a new feature branch"

```
Starting work on rate limiting refactor.

Let me check EXAMPLES.md for a similar task...
[reads examples for guidance]

/release-workflow bump patch  (when done)
```

---

### Pattern 3: "Database schema change"

```
@claude /database

I need to add a circuit_breaker_state table.

What's the migration file format?
How do I test UP and DOWN?
```

**Agent response:**
1. Loads database.md
2. Shows migration file format
3. Gives SQL template
4. Explains testing process

---

## How to Maintain These Guides

### When Code Changes

If you modify:
- **Handler patterns** → Update `.github/instructions/handler-development.md`
- **Provider interface** → Update `.github/instructions/provider-integration.md`
- **Frontend style** → Update `.github/instructions/frontend-components.md`
- **Migration workflow** → Update `.github/instructions/database.md`

### When Adding New Features

1. Create implementation plan: `20260410-NN - [Feature].md` in `docs/implementation_plans/`
2. Update relevant specialized guide with new pattern
3. Add example to `.github/instructions/EXAMPLES.md` if it's a common task
4. Link from main copilot-instructions.md if it's important

### Version Updates

Files are versioned via the main `copilot-instructions.md` footer:
```
Generated for AI agents on April 2026. Last sync with project repository.
```

Update when:
- Major architectural change
- New pattern becomes standard
- Significant codebase refactor

---

## FAQ

### Q: Where do I start if I'm new?

**A:** Read these in order:
1. [.github/copilot-instructions.md](.github/copilot-instructions.md) (5 min)
2. [.github/instructions/README.md](.github/instructions/README.md) (3 min)
3. Choose your path based on the task

### Q: How do I know which instruction file to use?

**A:** Check [.github/instructions/README.md](.github/instructions/README.md) — it has a scenario matrix showing which guide for each task.

### Q: Can I use agent modes without the agent feature?

**A:** Yes! Check the `applyToPatterns` and `instructions` in [.agent.md](.agent.md) to see which guides apply to your file type. Read those guides manually.

### Q: What if the instructions are outdated?

**A:** Please update them! They're living documents. If you discover:
- A pattern differs from code
- Examples are wrong
- New patterns exist

Update the corresponding guide file and the footer timestamp in copilot-instructions.md.

### Q: How do I reference these in prompts?

**A:**
```
Reference: .github/instructions/backend.md #Development-Workflow
See: handler-development.md #The-Handler-Pattern
Follow: .agent.md for agent modes
Use: docs/implementation_plans/TEMPLATE.md for planning
```

---

## Architecture Decision

### Why Split Into Multiple Files?

1. **Parallel reading** — Multiple people can reference different guides
2. **Focused context** — Easier to find your specific needs
3. **Shallow TOC** — Each guide ~15 min to read, not overwhelming
4. **Links, not embeds** — Reduce duplication, single source of truth for patterns
5. **Scalable** — Easy to add new guides; existing ones don't bloat

### Why Agents & Modes?

1. **Context filtering** — Backend specialist doesn't see frontend file globs
2. **Single responsibility** — Each mode loads only relevant guides
3. **Flexible** — Use general mode or specific modes as situation demands
4. **Defaults** — General mode is default; modes are opt-in enhancement

### Why a Release Skill?

1. **Automation** — Reduces manual versioning errors
2. **Consistency** — Same process every release
3. **Traceability** — Git tags + conventional commits
4. **Idempotent** — Safe to run multiple times

---

## Integration with Existing Docs

This instruction setup **complements** existing documentation:

```
copilot-instructions.md (high-level project overview)
    ↓ links to
AGENTS.md (general patterns)
    ↓ linked from
.github/instructions/* (specialized guides)
    ↓ reference
backend-go/AGENTS.md (package-specific patterns)
frontend/AGENTS.md
internal/handlers/AGENTS.md
```

**Principle**: Link, don't duplicate. Each file is single source of truth for its domain.

---

## Next Steps

### For Immediate Use

1. ✅ Guides are ready
2. ✅ Agent modes configured
3. ✅ Skill automated

Start using them:
```
@claude /backend

I'm working on [task]. 
Reference: .github/instructions/[guide].md
```

### For Team Onboarding

1. Add link to `.github/copilot-instructions.md` in team wiki
2. New devs read README + main instructions
3. Specify agent mode in task descriptions: `@claude /frontend`

### For Maintenance

1. Update guides when patterns change
2. Add examples to EXAMPLES.md for common tasks
3. Keep timestamps in footer current

---

## Summary

| What | Where | Used For |
|------|-------|----------|
| **Main Instructions** | `.github/copilot-instructions.md` | Project overview, entry point |
| **Navigation** | `.github/instructions/README.md` | Finding the right guide |
| **Specialist Guides (5)** | `.github/instructions/*.md` | Deep dives into each area |
| **Examples** | `.github/instructions/EXAMPLES.md` | Real prompt templates |
| **Agent Config** | `.agent.md` | Multi-role agent modes |
| **Release Skill** | `.claude/skills/release-workflow/SKILL.md` | Version automation |
| **Plan Template** | `docs/implementation_plans/TEMPLATE.md` | Feature planning |

**Everything is interconnected**, using links not duplication. **Ready to use today.**

---

**Created**: April 2026  
**Status**: ✅ Complete & Ready to Use  
**For**: AI agents, developers, teams using Copilot for cc-bridge development

---

## Questions or Issues?

If you find:
- **Broken links** — Update `.md` files
- **Outdated patterns** — Refresh the corresponding guide
- **Missing topics** — Add new guide (`.github/instructions/new-topic.md`)
- **Unclear sections** — Rewrite for clarity, ask for help

The guides are yours to maintain and improve. Keep them fresh, keep them linked, keep them helpful.
