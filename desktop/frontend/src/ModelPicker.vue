<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Cable, Check, ChevronDown, Cpu } from '@lucide/vue'
import type { Model } from './native'

const props = defineProps<{ model: Model; groups: { id: string; label: string; models: Model[] }[]; disabled: boolean }>()
const emit = defineEmits<{ select: [id: string]; opened: [] }>()
const trigger = ref<HTMLButtonElement>()
const menu = ref<HTMLDivElement>()
const open = ref(false)
const highlighted = ref('')
let typeahead = ''
let lastTyped = 0
const options = computed(() => props.groups.flatMap(group => group.models.filter(model => model.ready)))
const channel = computed(() => props.groups.find(group => group.id === props.model.channelId)?.label || '')
const showChannel = computed(() => channel.value && channel.value !== props.model.label && channel.value !== props.model.name)

function optionID(id: string) {
  return 'model-option-' + id
}

function positionMenu() {
  const button = trigger.value
  const panel = menu.value
  if (!button || !panel) return
  const bounds = button.getBoundingClientRect()
  const width = Math.min(326, window.innerWidth - 16)
  const above = bounds.top >= 230 || bounds.top > window.innerHeight - bounds.bottom
  const room = above ? bounds.top : window.innerHeight - bounds.bottom
  panel.style.width = width + 'px'
  panel.style.maxHeight = Math.max(0, Math.min(370, room - 14)) + 'px'
  panel.style.left = Math.max(8, Math.min(bounds.left, window.innerWidth - width - 8)) + 'px'
  panel.style.top = above ? 'auto' : bounds.bottom + 6 + 'px'
  panel.style.bottom = above ? window.innerHeight - bounds.top + 6 + 'px' : 'auto'
}

function focusOption(id: string) {
  highlighted.value = id
  void nextTick(() => document.getElementById(optionID(id))?.scrollIntoView({ block: 'nearest' }))
}

function show(direction = 1) {
  const panel = menu.value
  if (props.disabled || !panel || panel.matches(':popover-open')) return
  typeahead = ''
  lastTyped = 0
  positionMenu()
  const start = options.value.find(model => model.id === props.model.id)
  highlighted.value = start?.id || options.value.at(direction > 0 ? 0 : -1)?.id || ''
  panel.showPopover()
  open.value = true
  panel.focus({ preventScroll: true })
  if (highlighted.value) focusOption(highlighted.value)
  emit('opened')
}

function close(restore = false) {
  if (menu.value?.matches(':popover-open')) menu.value.hidePopover()
  open.value = false
  if (restore) trigger.value?.focus({ preventScroll: true })
}

function toggle() {
  if (open.value) close(true)
  else show()
}

function select(model: Model) {
  if (props.disabled || !model.ready) return
  close(true)
  if (model.id !== props.model.id) emit('select', model.id)
}

function triggerKey(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    show(event.key === 'ArrowDown' ? 1 : -1)
  }
}

function menuKey(event: KeyboardEvent) {
  if (event.key === 'Escape' || event.key === 'Tab') {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
    }
    close(true)
    return
  }
  const models = options.value
  if (!models.length) return
  const index = models.findIndex(model => model.id === highlighted.value)
  switch (event.key) {
    case 'ArrowDown':
    case 'ArrowUp': {
      const offset = event.key === 'ArrowDown' ? 1 : -1
      const origin = index < 0 && offset < 0 ? 0 : index
      const next = (origin + offset + models.length) % models.length
      focusOption(models[next].id)
      break
    }
    case 'Home': focusOption(models[0].id); break
    case 'End': focusOption(models[models.length - 1].id); break
    case 'Enter':
    case ' ':
      if (index >= 0) select(models[index])
      break
    default: {
      if (event.key.length !== 1 || event.ctrlKey || event.altKey || event.metaKey || event.isComposing) return
      const now = performance.now()
      typeahead = (now - lastTyped > 650 ? '' : typeahead) + event.key.toLocaleLowerCase()
      lastTyped = now
      const ordered = [...models.slice(index + 1), ...models.slice(0, index + 1)]
      const match = ordered.find(model => [model.label, model.name].some(label => label.toLocaleLowerCase().startsWith(typeahead)))
      if (match) focusOption(match.id)
    }
  }
  event.preventDefault()
}

function syncOpen() {
  open.value = !!menu.value?.matches(':popover-open')
}

function resize() {
  if (open.value) positionMenu()
}

watch(() => props.disabled, disabled => { if (disabled) close() })
onMounted(() => window.addEventListener('resize', resize))
onBeforeUnmount(() => window.removeEventListener('resize', resize))
defineExpose({ close })
</script>

<template>
  <div class="model-picker">
    <button ref="trigger" class="model-picker-trigger" type="button" aria-haspopup="listbox" :aria-expanded="open"
      aria-controls="model-picker-options" popovertarget="model-picker-options" :aria-disabled="disabled"
      :aria-label="'当前模型 · ' + (model.label || '选择模型')"
      :title="[model.label, showChannel ? channel : ''].filter(Boolean).join(' · ') || '选择模型'" @click.prevent="toggle" @keydown="triggerKey">
      <Cpu aria-hidden="true" /><span class="model-picker-name">{{ model.label || '选择模型' }}</span>
      <span v-if="showChannel" class="model-picker-channel">{{ channel }}</span><ChevronDown class="model-picker-chevron" aria-hidden="true" />
    </button>
    <div id="model-picker-options" ref="menu" class="model-picker-menu" popover="auto" role="listbox" aria-label="选择模型" tabindex="-1"
      :aria-activedescendant="open && highlighted ? optionID(highlighted) : undefined" @toggle="syncOpen" @keydown="menuKey">
      <div v-for="(group, index) in groups" :key="group.id" class="model-picker-group" role="group" :aria-labelledby="'model-picker-group-' + index">
        <div :id="'model-picker-group-' + index" class="model-picker-heading">
          <Cable aria-hidden="true" /><span>{{ group.label }}</span>
          <small v-if="group.models.some(item => item.label === group.label)">渠道</small>
        </div>
        <div v-for="item in group.models" :id="optionID(item.id)" :key="item.id" class="model-picker-option" role="option"
          :aria-selected="item.id === model.id" :aria-disabled="!item.ready" :data-highlighted="item.id === highlighted"
          :title="item.name === item.label ? item.label : item.label + ' · ' + item.name" @click="select(item)">
          <span class="model-picker-description">
            <strong>{{ item.label }}</strong><small v-if="item.name !== item.label">{{ item.name }}</small>
          </span>
          <span v-if="!item.ready" class="model-picker-unavailable">未就绪</span>
          <Check v-else-if="item.id === model.id" class="model-picker-check" aria-hidden="true" />
        </div>
      </div>
    </div>
  </div>
</template>
