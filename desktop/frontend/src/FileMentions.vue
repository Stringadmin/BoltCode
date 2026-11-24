<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { FileCode2, LoaderCircle } from '@lucide/vue'
import { native } from './native'

const props = defineProps<{ editor?: HTMLTextAreaElement; chatId: string; repository: string; selected: string[] }>()
const emit = defineEmits<{ pick: [path: string, start: number, end: number] }>()
const open = ref(false)
const waiting = ref(false)
const failed = ref(false)
const paths = ref<string[]>([])
const active = ref(0)
let generation = 0
let range: { start: number; end: number; query: string } | undefined
let debounce: ReturnType<typeof setTimeout> | undefined

function dismiss() {
  generation++
  clearTimeout(debounce)
  open.value = false
  range = undefined
}

function update() {
  const editor = props.editor
  if (!props.repository || !editor || editor.selectionStart !== editor.selectionEnd || props.selected.length >= 6) {
    dismiss()
    return
  }
  const end = editor.selectionStart
  const match = /(?:^|[^\w./@])@([^\s@]*)$/.exec(editor.value.slice(0, end))
  if (!match) {
    dismiss()
    return
  }
  const query = match[1]
  if (open.value && range?.end === end && range.query === query) return
  range = { start: end - query.length - 1, end, query }
  const request = ++generation
  clearTimeout(debounce)
  open.value = true
  waiting.value = true
  failed.value = false
  active.value = 0
  paths.value = []
  debounce = setTimeout(async () => {
    try {
      const results = await native<string[]>('SearchFiles', props.chatId, query)
      if (request === generation) paths.value = results.filter(path => !props.selected.includes(path))
    } catch {
      if (request === generation) failed.value = true
    } finally {
      if (request === generation) waiting.value = false
    }
  }, 130)
}

function pick(path: string) {
  if (range) emit('pick', path, range.start, range.end)
  dismiss()
}

function handleKey(event: KeyboardEvent) {
  if (!open.value || event.isComposing || event.keyCode === 229) return false
  if (event.key === 'Escape') {
    dismiss()
    event.stopPropagation()
  } else if (event.key === 'Enter' && !event.shiftKey) {
    if (paths.value[active.value]) pick(paths.value[active.value])
  } else if ((event.key === 'ArrowUp' || event.key === 'ArrowDown') && paths.value.length) {
    const offset = event.key === 'ArrowUp' ? -1 : 1
    active.value = (active.value + offset + paths.value.length) % paths.value.length
    void nextTick(() => document.getElementById('reference-option-' + active.value)?.scrollIntoView({ block: 'nearest' }))
  } else {
    return false
  }
  event.preventDefault()
  return true
}

watch(() => [props.repository, props.chatId], dismiss)
onBeforeUnmount(dismiss)
defineExpose({ update, dismiss, handleKey, open, active, hasOptions: () => !!paths.value.length })
</script>

<template>
  <div v-if="open" class="mention-picker" @mousedown.prevent>
    <p v-if="waiting" class="mention-empty" role="status"><LoaderCircle class="spinning" /> 正在查找文件…</p>
    <p v-else-if="!paths.length" class="mention-empty" role="status">{{ failed ? '无法读取工作区文件' : '没有匹配的文件' }}</p>
    <div id="reference-options" class="mention-list" role="listbox" aria-label="引用工作区文件" :hidden="!paths.length">
      <button v-for="(path, index) in paths" :id="'reference-option-' + index" :key="path" type="button" role="option" tabindex="-1"
        :aria-selected="index === active" :title="path" @mouseenter="active = index" @click="pick(path)">
        <FileCode2 aria-hidden="true" /><span>{{ path }}</span>
      </button>
    </div>
  </div>
</template>
