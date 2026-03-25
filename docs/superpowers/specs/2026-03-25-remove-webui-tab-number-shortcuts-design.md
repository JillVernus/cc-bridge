# Remove WebUI Tab Number Shortcuts Design

## Goal

Stop the top navigation tabs in the WebUI from switching when the user presses
number keys such as `1`, `2`, `3`, or `4`.

## Current State

`frontend/src/App.vue` registers a global `keydown` handler on `window`.
That handler currently does two separate things:

1. Ignores shortcuts while the user is typing in an input-like element.
2. Closes open dialogs on `Escape`.
3. Switches authenticated top-level tabs when number keys are pressed.

The numeric tab switching is not useful for this product and can trigger
unwanted UI changes during normal use.

## Proposed Change

Keep the existing global `Escape` behavior exactly as-is.
Remove only the conditional block that maps number keys to `activeTab`.

## Scope

In scope:

- `frontend/src/App.vue`
- Targeted frontend verification with existing repo commands

Out of scope:

- Any change to `Escape` shortcut handling
- Adding new shortcut behavior
- Introducing a frontend test framework just for this change

## Validation

This frontend does not currently have a test framework configured, so validation
will use the smallest existing checks:

- `bun run type-check`
- `bun run build`
