# Theme Switching Implementation

## Status: In Progress

## Background
User wanted to add real-time theme switching with a completely different UI style (not just color changes). Currently bored with the "Retro Pixel" theme and works at night, so needs dark themes.

## What Was Done

### 1. Added Vuetify Theme (`frontend/src/plugins/vuetify.ts`)
- Added `minimalDarkTheme` with Zinc color palette (neutral grays)

### 2. Theme Switching Logic (`frontend/src/App.vue`)
- Added `ThemeOption` type: `'retro-light' | 'retro-dark' | 'minimal-dark'`
- Added `setTheme()` function that sets both Vuetify theme and `data-theme` attribute
- Theme persisted to `localStorage.themeOption`
- Default theme: `retro-dark`

### 3. Theme Selector UI (`frontend/src/App.vue`)
- Replaced simple dark/light toggle with dropdown menu (palette icon)
- Shows 3 options: Retro Light, Retro Dark, Minimal Dark

### 4. CSS Theme System (`frontend/src/App.vue`)
- Global styles scoped with `[data-theme="retro"]` and `[data-theme="minimal"]`
- Added minimal theme overrides for scoped styles using `:global([data-theme="minimal"])`
- Fixed background color issue - cream (#fffbeb) now only applies to retro-light

### 5. i18n (`frontend/src/locales/en.ts`, `zh-CN.ts`)
- Added theme labels in both languages

## Current Issue

The "Minimal Dark" theme is NOT truly minimal - it still has:
- Thick borders (2px)
- Hard offset shadows (4px 4px 0 0)
- Monospace fonts
- Sharp corners

This is because the scoped CSS (~600 lines) applies retro styles unconditionally, then the minimal overrides try to undo them. The current "minimal-dark" is really just "Retro Deep Dark" (darker color palette).

## User Feedback
> "looks good, i love this one, but it is still the one i call 'change color', as it is still retro style, i may call this one is retro deep dark."

## Next Steps (Two Options)

### Option A: Rename Only
- Rename "Minimal Dark" to "Retro Deep Dark"
- Implement true minimal theme later

### Option B: Full Implementation
- Go through all ~600 lines of scoped CSS in App.vue
- Properly scope every retro-specific style to `[data-theme="retro"]`
- Let minimal theme use clean Vuetify defaults (rounded corners, soft shadows, system fonts)

## Files Modified
- `frontend/src/plugins/vuetify.ts` - Added minimalDarkTheme
- `frontend/src/App.vue` - Theme logic, UI menu, CSS overrides
- `frontend/src/locales/en.ts` - English translations
- `frontend/src/locales/zh-CN.ts` - Chinese translations

## TypeScript Check
Passed ✓

## Git Status
Changes not committed yet. Run `git diff --stat` to see changes.

## Completion Status (2026-01-11)
- Implemented Option A + Option B as requested.
- Renamed "Minimal Dark" to "Retro Deep Dark" (keeps retro styling + zinc colors).
- Implemented true "Minimal Dark" theme (zinc colors + clean styling).
- Refactored `App.vue` CSS to properly scope all retro styles to `[data-theme="retro"]`.
- The application now supports 4 themes:
  1. Retro Light (Cream bg)
  2. Retro Dark (Slate bg)
  3. Retro Deep Dark (Zinc bg)
  4. Minimal Dark (Zinc bg, no retro borders/shadows)

