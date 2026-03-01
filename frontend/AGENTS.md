# FRONTEND

Vue 3 + Vuetify 3 admin UI. Built with Vite, embedded into Go binary.

## STRUCTURE

```
frontend/
├── src/
│   ├── App.vue              # Root component (1400+ lines)
│   ├── main.ts              # Vue app bootstrap
│   ├── components/          # 19 Vue SFCs
│   ├── composables/         # useLocale, useLogStream, useTheme
│   ├── services/            # API client
│   ├── plugins/             # Vuetify, i18n
│   └── locales/             # en.json, zh.json
├── index.html
└── vite.config.ts
```

## COMPONENTS

| Component | Purpose |
|-----------|---------|
| `ChannelOrchestration.vue` | Main channel management UI |
| `AddChannelModal.vue` | Create/edit channel dialog |
| `RequestLogTable.vue` | Request history table |
| `APIKeyManagement.vue` | API key CRUD |
| `PricingSettings.vue` | Token pricing config |
| `RateLimitSettings.vue` | Rate limit config |
| `ForwardProxySettings.vue` | Forward proxy config |
| `ChannelStatsChart.vue` | Per-channel metrics |
| `GlobalStatsChart.vue` | Aggregate stats |

## CONVENTIONS

### Vue Style
- Composition API with `<script setup>`
- No Options API
- Props via `defineProps<{...}>()`
- Emits via `defineEmits<{...}>()`

### Styling
- Vuetify 3 components (v-btn, v-card, v-data-table)
- Tailwind CSS utilities for custom styles
- CSS Grid/Flexbox for layouts

### Formatting (Prettier)
```json
{
  "semi": false,
  "singleQuote": true,
  "trailingComma": "none",
  "printWidth": 120
}
```

## COMPOSABLES

| Composable | Purpose |
|------------|---------|
| `useLocale.ts` | i18n locale switching |
| `useLogStream.ts` | SSE connection for live logs |
| `useTheme.ts` | Dark/light theme toggle |

## API CLIENT

Located in `src/services/`. Uses Fetch API.
- Base URL: same origin (embedded)
- Auth: `x-api-key` header

## COMMANDS

```bash
bun install        # Install deps
bun run dev        # Vite dev server (port 5173)
bun run build      # Production build → dist/
bun run type-check # TypeScript check
```

## NOTES

- No test framework configured
- Build output embedded via Go's `//go:embed`
- Hot-reload in dev mode via Vite
