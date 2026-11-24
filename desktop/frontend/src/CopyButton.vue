<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { Check, CircleAlert, Copy } from '@lucide/vue'
import { copyText } from './native'

const props = defineProps<{ text: string; label: string }>()
const state = ref<'idle' | 'copying' | 'copied' | 'failed'>('idle')
const feedback = computed(() => state.value === 'copied' ? '已复制' : state.value === 'failed' ? '复制失败' : '')
let timer: ReturnType<typeof setTimeout> | undefined
let version = 0

function reset() {
  version++
  clearTimeout(timer)
  state.value = 'idle'
}

async function copy() {
  const request = ++version
  clearTimeout(timer)
  state.value = 'copying'
  try {
    await copyText(props.text)
    if (request !== version) return
    state.value = 'copied'
  } catch {
    if (request !== version) return
    state.value = 'failed'
  }
  timer = setTimeout(reset, 1600)
}

watch(() => props.text, reset)
onUnmounted(reset)
</script>

<template>
  <button class="copy-button quiet-icon" type="button" :aria-label="label"
    :title="feedback || label" :disabled="!text || state === 'copying'" @click="copy">
    <Check v-if="state === 'copied'" class="success" />
    <CircleAlert v-else-if="state === 'failed'" class="failure" />
    <Copy v-else />
    <span class="sr-only" role="status">{{ feedback }}</span>
  </button>
</template>
