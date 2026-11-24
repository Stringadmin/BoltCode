<script setup lang="ts">
import { CircleAlert, CircleCheck, MessageSquare } from '@lucide/vue'
import type { Chat } from './native'

defineProps<{ chat: Chat; selected: boolean; running: boolean; checking: boolean }>()
const emit = defineEmits<{ select: []; menu: [event: MouseEvent | KeyboardEvent] }>()
</script>

<template>
  <button class="thread" type="button" :aria-pressed="selected" :aria-label="chat.title" :title="chat.title"
    aria-haspopup="menu" @click="emit('select')" @contextmenu.prevent="emit('menu', $event)"
    @keydown.shift.f10.prevent="emit('menu', $event)" @keydown.context-menu.prevent="emit('menu', $event)">
    <MessageSquare /><span>{{ chat.title }}</span>
    <span v-if="running || checking" class="running-dot" :aria-label="checking ? '验证中' : '运行中'"></span>
    <CircleCheck v-else-if="chat.merged" class="success" aria-label="已合并" />
    <CircleAlert v-else-if="chat.runState === 'failed' || chat.runState === 'interrupted'" class="muted"
      :aria-label="chat.runState === 'failed' ? '执行失败' : '已停止'" />
  </button>
</template>
