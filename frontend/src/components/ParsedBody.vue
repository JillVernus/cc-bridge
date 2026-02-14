<template>
  <div class="parsed-body">
    <!-- Parse error fallback -->
    <div v-if="parseError" class="text-medium-emphasis text-caption">
      {{ parseError }}
    </div>

    <!-- Request view -->
    <template v-else-if="type === 'request'">
      <!-- Params row -->
      <div v-if="parsed.params.length" class="params-row mb-3">
        <v-chip
          v-for="p in parsed.params"
          :key="p.label"
          size="small"
          variant="tonal"
          class="mr-1 mb-1"
        >
          <span class="text-medium-emphasis mr-1">{{ p.label }}:</span>
          <span class="font-weight-medium">{{ p.value }}</span>
        </v-chip>
      </div>

      <!-- System prompt -->
      <div v-if="parsed.system" class="mb-3">
        <div class="text-caption text-medium-emphasis mb-1">System</div>
        <v-card variant="tonal" color="grey" class="pa-2">
          <div class="message-text">{{ parsed.system }}</div>
        </v-card>
      </div>

      <!-- Messages -->
      <div v-if="parsed.messages.length">
        <div class="text-caption text-medium-emphasis mb-1">Messages</div>
        <div v-for="(msg, i) in parsed.messages" :key="i" class="mb-2">
          <v-card
            variant="tonal"
            :color="msg.role === 'user' ? 'blue' : msg.role === 'assistant' ? 'green' : 'grey'"
            class="pa-2"
          >
            <div class="d-flex align-center mb-1">
              <v-icon size="x-small" class="mr-1">
                {{ msg.role === 'user' ? 'mdi-account' : msg.role === 'assistant' ? 'mdi-robot' : 'mdi-cog' }}
              </v-icon>
              <span class="text-caption font-weight-bold">{{ msg.role }}</span>
            </div>
            <div class="message-text">{{ msg.content }}</div>
          </v-card>
        </div>
      </div>

      <!-- Tools -->
      <div v-if="parsed.tools.length" class="mt-2">
        <div class="text-caption text-medium-emphasis mb-1">Tools ({{ parsed.tools.length }})</div>
        <v-chip v-for="tool in parsed.tools" :key="tool" size="small" variant="outlined" class="mr-1 mb-1">
          {{ tool }}
        </v-chip>
      </div>
    </template>

    <!-- Response view -->
    <template v-else>
      <!-- Params row -->
      <div v-if="parsed.params.length" class="params-row mb-3">
        <v-chip
          v-for="p in parsed.params"
          :key="p.label"
          size="small"
          variant="tonal"
          class="mr-1 mb-1"
        >
          <span class="text-medium-emphasis mr-1">{{ p.label }}:</span>
          <span class="font-weight-medium">{{ p.value }}</span>
        </v-chip>
      </div>

      <!-- Content -->
      <div v-if="parsed.content" class="mb-2">
        <div class="text-caption text-medium-emphasis mb-1">Content</div>
        <v-card variant="tonal" color="green" class="pa-2">
          <div class="message-text">{{ parsed.content }}</div>
        </v-card>
      </div>

      <!-- Tool use blocks -->
      <div v-if="parsed.toolUse.length" class="mt-2">
        <div class="text-caption text-medium-emphasis mb-1">Tool Use</div>
        <v-card v-for="(tu, i) in parsed.toolUse" :key="i" variant="tonal" color="orange" class="pa-2 mb-2">
          <div class="text-caption font-weight-bold mb-1">{{ tu.name }}</div>
          <pre class="tool-input">{{ tu.input }}</pre>
        </v-card>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  body: string
  type: 'request' | 'response'
}>()

interface Param { label: string; value: string }
interface Message { role: string; content: string }
interface ToolUse { name: string; input: string }

interface ParsedResult {
  params: Param[]
  system: string
  messages: Message[]
  tools: string[]
  content: string
  toolUse: ToolUse[]
}

const emptyResult: ParsedResult = {
  params: [], system: '', messages: [], tools: [], content: '', toolUse: []
}

const parseError = computed(() => {
  if (!props.body) return 'No body data'
  try {
    parseBody(props.body, props.type)
    return null
  } catch {
    return 'Unable to parse body'
  }
})

const parsed = computed<ParsedResult>(() => {
  if (!props.body) return { ...emptyResult }
  try {
    return parseBody(props.body, props.type)
  } catch {
    return { ...emptyResult }
  }
})

function parseBody(raw: string, type: 'request' | 'response'): ParsedResult {
  // Try SSE stream first (response only)
  if (type === 'response' && raw.includes('event:') && raw.includes('data:')) {
    return parseSSEResponse(raw)
  }

  const obj = JSON.parse(raw)

  if (type === 'request') {
    return parseRequest(obj)
  } else {
    return parseResponse(obj)
  }
}

function parseRequest(obj: Record<string, unknown>): ParsedResult {
  const params: Param[] = []
  const messages: Message[] = []
  let system = ''
  const tools: string[] = []

  if (obj.model) params.push({ label: 'model', value: String(obj.model) })
  if (obj.max_tokens) params.push({ label: 'max_tokens', value: String(obj.max_tokens) })
  if (obj.temperature != null) params.push({ label: 'temperature', value: String(obj.temperature) })
  if (obj.top_p != null) params.push({ label: 'top_p', value: String(obj.top_p) })
  if (obj.stream != null) params.push({ label: 'stream', value: String(obj.stream) })

  // System prompt - Claude format (string or array)
  if (obj.system) {
    if (typeof obj.system === 'string') {
      system = obj.system
    } else if (Array.isArray(obj.system)) {
      system = (obj.system as Array<{ text?: string }>)
        .map(b => b.text || JSON.stringify(b))
        .join('\n')
    }
  }
  // OpenAI format: system message in messages array
  if (Array.isArray(obj.messages)) {
    for (const msg of obj.messages as Array<Record<string, unknown>>) {
      const role = String(msg.role || 'unknown')
      if (role === 'system' && !system) {
        system = extractContent(msg.content)
        continue
      }
      messages.push({ role, content: extractContent(msg.content) })
    }
  }

  // Tools
  if (Array.isArray(obj.tools)) {
    for (const tool of obj.tools as Array<Record<string, unknown>>) {
      const name = (tool.name as string)
        || ((tool.function as Record<string, unknown>)?.name as string)
        || 'unknown'
      tools.push(name)
    }
  }

  return { params, system, messages, tools, content: '', toolUse: [] }
}

function parseResponse(obj: Record<string, unknown>): ParsedResult {
  const params: Param[] = []
  let content = ''
  const toolUse: ToolUse[] = []

  if (obj.model) params.push({ label: 'model', value: String(obj.model) })
  if (obj.stop_reason) params.push({ label: 'stop_reason', value: String(obj.stop_reason) })
  if (obj.finish_reason) params.push({ label: 'finish_reason', value: String(obj.finish_reason) })

  // Usage
  const usage = obj.usage as Record<string, unknown> | undefined
  if (usage) {
    const parts: string[] = []
    if (usage.input_tokens != null) parts.push(`${usage.input_tokens} in`)
    if (usage.prompt_tokens != null) parts.push(`${usage.prompt_tokens} in`)
    if (usage.output_tokens != null) parts.push(`${usage.output_tokens} out`)
    if (usage.completion_tokens != null) parts.push(`${usage.completion_tokens} out`)
    if (usage.cache_read_input_tokens) parts.push(`${usage.cache_read_input_tokens} cache`)
    if (parts.length) params.push({ label: 'tokens', value: parts.join(' / ') })
  }

  // Claude format content blocks
  if (Array.isArray(obj.content)) {
    const textParts: string[] = []
    for (const block of obj.content as Array<Record<string, unknown>>) {
      if (block.type === 'text') {
        textParts.push(String(block.text || ''))
      } else if (block.type === 'tool_use') {
        toolUse.push({
          name: String(block.name || 'unknown'),
          input: typeof block.input === 'string' ? block.input : JSON.stringify(block.input, null, 2)
        })
      }
    }
    content = textParts.join('\n')
  }

  // OpenAI format choices
  if (Array.isArray(obj.choices)) {
    const textParts: string[] = []
    for (const choice of obj.choices as Array<Record<string, unknown>>) {
      const msg = choice.message as Record<string, unknown> | undefined
      if (msg?.content) textParts.push(String(msg.content))
      // Tool calls
      if (Array.isArray(msg?.tool_calls)) {
        for (const tc of msg.tool_calls as Array<Record<string, unknown>>) {
          const fn = tc.function as Record<string, unknown> | undefined
          toolUse.push({
            name: String(fn?.name || 'unknown'),
            input: String(fn?.arguments || '{}')
          })
        }
      }
    }
    content = textParts.join('\n')
  }

  // Codex Responses API format
  if (Array.isArray(obj.output)) {
    const textParts: string[] = []
    for (const item of obj.output as Array<Record<string, unknown>>) {
      if (item.type === 'message') {
        const msgContent = item.content
        if (Array.isArray(msgContent)) {
          for (const block of msgContent as Array<Record<string, unknown>>) {
            if (block.type === 'output_text') textParts.push(String(block.text || ''))
          }
        }
      }
    }
    if (textParts.length) content = textParts.join('\n')
  }

  return { params, system: '', messages: [], tools: [], content, toolUse }
}

function parseSSEResponse(raw: string): ParsedResult {
  const params: Param[] = []
  const textParts: string[] = []
  const toolUse: ToolUse[] = []
  let model = ''
  let stopReason = ''
  let usage: Record<string, unknown> | null = null

  // Accumulate tool_use input JSON fragments by index
  const toolInputBuffers: Map<number, { name: string; jsonParts: string[] }> = new Map()

  const lines = raw.split('\n')
  for (const line of lines) {
    if (!line.startsWith('data:')) continue
    const data = line.slice(5).trim()
    if (!data || data === '[DONE]') continue

    try {
      const evt = JSON.parse(data) as Record<string, unknown>
      const evtType = evt.type as string | undefined

      if (!model && evt.model) model = String(evt.model)

      // Claude SSE events
      if (evtType === 'content_block_delta') {
        const delta = evt.delta as Record<string, unknown> | undefined
        if (delta?.type === 'text_delta') {
          textParts.push(String(delta.text || ''))
        } else if (delta?.type === 'input_json_delta') {
          const idx = evt.index as number
          const buf = toolInputBuffers.get(idx)
          if (buf) buf.jsonParts.push(String(delta.partial_json || ''))
        }
      } else if (evtType === 'content_block_start') {
        const block = evt.content_block as Record<string, unknown> | undefined
        if (block?.type === 'tool_use') {
          toolInputBuffers.set(evt.index as number, {
            name: String(block.name || 'unknown'),
            jsonParts: []
          })
        }
      } else if (evtType === 'message_delta') {
        const delta = evt.delta as Record<string, unknown> | undefined
        if (delta?.stop_reason) stopReason = String(delta.stop_reason)
        if (evt.usage) usage = evt.usage as Record<string, unknown>
      } else if (evtType === 'message_start') {
        const msg = evt.message as Record<string, unknown> | undefined
        if (msg?.model) model = String(msg.model)
        if (msg?.usage) usage = msg.usage as Record<string, unknown>
      }

      // OpenAI SSE format
      if (Array.isArray(evt.choices)) {
        for (const choice of evt.choices as Array<Record<string, unknown>>) {
          const delta = choice.delta as Record<string, unknown> | undefined
          if (delta?.content) textParts.push(String(delta.content))
          if (choice.finish_reason) stopReason = String(choice.finish_reason)
        }
      }
    } catch {
      // skip unparseable lines
    }
  }

  // Finalize tool use from buffers
  for (const buf of toolInputBuffers.values()) {
    let input = buf.jsonParts.join('')
    try { input = JSON.stringify(JSON.parse(input), null, 2) } catch { /* keep raw */ }
    toolUse.push({ name: buf.name, input })
  }

  if (model) params.push({ label: 'model', value: model })
  if (stopReason) params.push({ label: 'stop_reason', value: stopReason })
  if (usage) {
    const parts: string[] = []
    if (usage.input_tokens != null) parts.push(`${usage.input_tokens} in`)
    if (usage.output_tokens != null) parts.push(`${usage.output_tokens} out`)
    if (usage.cache_read_input_tokens) parts.push(`${usage.cache_read_input_tokens} cache`)
    if (parts.length) params.push({ label: 'tokens', value: parts.join(' / ') })
  }

  return {
    params,
    system: '',
    messages: [],
    tools: [],
    content: textParts.join(''),
    toolUse
  }
}

function extractContent(content: unknown): string {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((b: Record<string, unknown>) => {
        if (b.type === 'text') return String(b.text || '')
        if (b.type === 'image') return '[image]'
        if (b.type === 'image_url') return '[image]'
        if (b.type === 'tool_result') {
          const inner = b.content
          if (typeof inner === 'string') return inner
          if (Array.isArray(inner)) {
            return (inner as Array<Record<string, unknown>>)
              .map(ib => ib.type === 'text' ? String(ib.text || '') : `[${ib.type}]`)
              .join('\n')
          }
          return `[tool_result: ${b.tool_use_id}]`
        }
        if (b.type === 'tool_use') return `[tool_use: ${b.name}]`
        return `[${b.type || 'unknown'}]`
      })
      .join('\n')
  }
  if (content && typeof content === 'object') return JSON.stringify(content, null, 2)
  return String(content ?? '')
}
</script>

<style scoped>
.message-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 0.8rem;
  line-height: 1.5;
}

.tool-input {
  margin: 0;
  font-size: 0.75rem;
  font-family: 'Courier New', Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow: auto;
}
</style>
