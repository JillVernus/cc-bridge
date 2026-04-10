# Frontend Component Development Guide

**Specialist guide for Vue 3 component work in cc-bridge.**

---

## Quick Facts

- **Framework**: Vue 3 Composition API (no Options API)
- **UI Library**: Vuetify 3 (Material Design)
- **Styling**: Tailwind CSS + scoped `<style>`
- **Package manager**: Bun (not npm/yarn)
- **Dev server**: Vite (port 5173)
- **Type safety**: TypeScript (strict mode)
- **Formatter**: Prettier (120 char width, no semicolons)
- **Components**: ~19 SFCs in `src/components/`
- **Composables**: `useLocale`, `useLogStream`, `useTheme`

---

## Composition API Pattern

All components use `<script setup>` syntax (no Options API):

```vue
<!-- src/components/MyComponent.vue -->
<template>
  <v-card class="p-4">
    <v-card-title>{{ title }}</v-card-title>
    <v-card-text>
      <v-text-field
        v-model="input"
        label="Enter text"
        @update:model-value="handleChange"
      />
    </v-card-text>
    <v-card-actions>
      <v-btn @click="submit">Submit</v-btn>
    </v-card-actions>
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

// Props with TypeScript typing
interface Props {
  title?: string
  items: string[]
}
const props = withDefaults(defineProps<Props>(), {
  title: 'Default Title'
})

// Emits with TypeScript typing
const emit = defineEmits<{
  select: [id: string]
  close: []
}>()

// Reactive state
const input = ref('')
const isLoading = ref(false)

// Computed property
const hasItems = computed(() => props.items.length > 0)

// Methods
const handleChange = (value: string) => {
  input.value = value
}

const submit = async () => {
  isLoading.value = true
  try {
    // Call API
    emit('select', input.value)
  } finally {
    isLoading.value = false
  }
}
</script>

<style scoped>
/* Scoped styles only */
:deep(.v-card) {
  /* Override child component styles if needed */
}
</style>
```

**Key points:**
- No `this` — use `ref()` for state, `computed()` for derived values
- `defineProps<Type>()` for type-safe props
- `defineEmits<{event: [params]}>()` for typed events
- `:deep()` only when overriding child component styles
- Automatic `expose` if using `ref` patterns

---

## Common Patterns

### Using Composables

```vue
<script setup lang="ts">
import { useLocale } from '@/composables/useLocale'
import { useTheme } from '@/composables/useTheme'

const { locale, setLocale } = useLocale()
const { isDark, toggleDark } = useTheme()
</script>
```

### API Calls

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { apiService } from '@/services/api'

const data = ref<DataType | null>(null)
const error = ref<string | null>(null)
const isLoading = ref(false)

const fetchData = async () => {
  isLoading.value = true
  try {
    data.value = await apiService.getData()
    error.value = null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Unknown error'
    data.value = null
  } finally {
    isLoading.value = false
  }
}

onMounted(() => {
  fetchData()
})
</script>

<template>
  <div>
    <v-progress-circular v-if="isLoading" indeterminate />
    <v-alert v-if="error" type="error">{{ error }}</v-alert>
    <div v-if="data">{{ data }}</div>
  </div>
</template>
```

### Form with v-model

```vue
<script setup lang="ts">
import { ref } from 'vue'

interface FormData {
  name: string
  email: string
  enabled: boolean
}

const form = ref<FormData>({
  name: '',
  email: '',
  enabled: false
})

const submit = () => {
  console.log('Form submitted:', form.value)
}
</script>

<template>
  <v-form @submit.prevent="submit">
    <v-text-field
      v-model="form.name"
      label="Name"
      required
    />
    <v-text-field
      v-model="form.email"
      label="Email"
      type="email"
      required
    />
    <v-checkbox
      v-model="form.enabled"
      label="Enable this"
    />
    <v-btn type="submit" class="mt-4">Submit</v-btn>
  </v-form>
</template>
```

### Conditional Rendering & Lists

```vue
<template>
  <!-- v-if for conditional blocks -->
  <v-alert v-if="hasError" type="error">
    Something went wrong
  </v-alert>

  <!-- v-show for CSS display toggle (better for frequent changes) -->
  <v-progress-linear v-show="isLoading" />

  <!-- v-for with key -->
  <v-list>
    <v-list-item
      v-for="item in items"
      :key="item.id"
      @click="selectItem(item.id)"
    >
      <v-list-item-title>{{ item.name }}</v-list-item-title>
    </v-list-item>
  </v-list>

  <!-- Empty state -->
  <div v-if="items.length === 0" class="text-center py-8">
    <p class="text-gray-500">No items found</p>
  </div>
</template>
```

---

## Styling Guide

### Vuetify 3 Components

Use Vuetify for layout and UI structure:

```vue
<template>
  <!-- Cards for grouped content -->
  <v-card>
    <v-card-title>Title</v-card-title>
    <v-card-subtitle>Subtitle</v-card-subtitle>
    <v-card-text>Content</v-card-text>
    <v-card-actions>
      <v-btn>Action</v-btn>
    </v-card-actions>
  </v-card>

  <!-- Data tables -->
  <v-data-table
    :headers="headers"
    :items="rows"
    :items-per-page="10"
    @click:row="selectRow"
  />

  <!-- Dialogs -->
  <v-dialog v-model="showDialog">
    <v-card>
      <v-card-title>Dialog</v-card-title>
      <v-card-text>Content</v-card-text>
      <v-card-actions>
        <v-btn @click="showDialog = false">Close</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Forms -->
  <v-form ref="form" @submit.prevent="submit">
    <v-text-field v-model="email" label="Email" required />
  </v-form>
</template>
```

### Tailwind CSS for Custom Styles

Use Tailwind for spacing, colors, responsive layout:

```vue
<template>
  <div class="flex gap-4 p-6">
    <!-- Spacing: gap-4, p-6 (padding) -->
    <div class="w-1/3">Left panel</div>
    <div class="w-2/3">Right panel</div>
  </div>

  <div class="grid grid-cols-3 gap-2 md:grid-cols-6">
    <!-- Responsive grid: 3 cols on mobile, 6 on tablet+ -->
  </div>

  <button class="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600">
    Click me
  </button>

  <!-- Conditional classes -->
  <div :class="{ 'bg-red-100 border-red-500': hasError }">
    Error message
  </div>
</template>
```

### Scoped Styles Only

```vue
<style scoped>
/* Styles here only apply to this component */
.my-container {
  display: flex;
  gap: 1rem;
}

/* Use :deep() to style child components (sparingly) */
:deep(.v-data-table) {
  font-size: 0.875rem;
}

/* Media queries work fine */
@media (max-width: 640px) {
  .my-container {
    flex-direction: column;
  }
}
</style>
```

---

## Internationalization (i18n)

Use `useLocale()` composable for multi-language support:

```vue
<script setup lang="ts">
import { useLocale } from '@/composables/useLocale'

const { t } = useLocale()
</script>

<template>
  <v-card>
    <v-card-title>{{ t('channel.title') }}</v-card-title>
    <v-card-text>{{ t('channel.description') }}</v-card-text>
  </v-card>
</template>
```

Update translation files if adding new strings:

```json
{
  "channel": {
    "title": "Channel Management",
    "description": "Manage upstream channels"
  }
}
```

Files: `src/locales/en.json`, `src/locales/zh.json`

---

## API Client Integration

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { apiService } from '@/services/api'

interface Channel {
  id: string
  name: string
  status: 'active' | 'inactive'
}

const channels = ref<Channel[]>([])
const isLoading = ref(false)
const error = ref<string | null>(null)

const loadChannels = async () => {
  isLoading.value = true
  try {
    channels.value = await apiService.get<Channel[]>('/api/channels')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load channels'
  } finally {
    isLoading.value = false
  }
}

const createChannel = async (name: string) => {
  try {
    await apiService.post('/api/channels', { name })
    await loadChannels()  // Refresh list
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to create channel'
  }
}

onMounted(() => {
  loadChannels()
})
</script>

<template>
  <div>
    <v-alert v-if="error" type="error" class="mb-4">{{ error }}</v-alert>
    <v-progress-circular v-if="isLoading" indeterminate />
    <v-list v-else>
      <v-list-item
        v-for="channel in channels"
        :key="channel.id"
      >
        <v-list-item-title>{{ channel.name }}</v-list-item-title>
        <template #append>
          <v-chip :color="channel.status === 'active' ? 'green' : 'gray'">
            {{ channel.status }}
          </v-chip>
        </template>
      </v-list-item>
    </v-list>
  </div>
</template>
```

---

## Working with Existing Components

### Component Map

| Component | Purpose |
|-----------|---------|
| `ChannelOrchestration.vue` | Main channel management UI |
| `AddChannelModal.vue` | Create/edit channel dialog |
| `RequestLogTable.vue` | Request history table |
| `APIKeyManagement.vue` | API key CRUD |
| `PricingSettings.vue` | Token pricing config |
| `ChannelStatsChart.vue` | Per-channel metrics |

Open a component to understand its props, emits, and API usage.

---

## Development Workflow

```bash
cd frontend

# Install dependencies
bun install

# Start dev server (Vite, port 5173)
bun run dev

# Type-check TypeScript
bun run type-check

# Format code (Prettier, 120 chars, no semicolons)
bunx prettier --write .

# Build for production
bun run build
```

**Prettier config** is strict — run formatter before committing.

---

## Pre-Commit Checklist

- [ ] `bun run type-check` passes (no TypeScript errors)
- [ ] `bunx prettier --write .` applied (consistent formatting)
- [ ] Tested in dev server (`bun run dev`)
- [ ] No `<style>` tags outside `<style scoped>`
- [ ] Props and emits are typed with TypeScript
- [ ] Uses `useLocale()` for user-facing text (i18n ready)
- [ ] No `console.log()` statements in production code

---

## See Also

- **Vue 3 Composition API**: [Vue Docs](https://vuejs.org/guide/introduction.html)
- **Vuetify 3 Components**: [Vuetify Docs](https://vuetifyjs.com/)
- **Tailwind CSS**: [Tailwind Docs](https://tailwindcss.com/)
- **Frontend AGENTS.md**: [frontend/AGENTS.md](../../frontend/AGENTS.md)
- **Main frontend entry**: [src/App.vue](../../frontend/src/App.vue)

---

*Last updated: April 2026*
