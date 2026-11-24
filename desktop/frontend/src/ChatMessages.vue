<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown, CircleCheck, FileCode2, FileText, History, RotateCcw } from '@lucide/vue'
import CopyButton from './CopyButton.vue'
import MarkdownMessage from './MarkdownMessage.vue'
import TurnActivity from './TurnActivity.vue'
import WorkspaceMemoryList from './WorkspaceMemoryList.vue'
import type { Message } from './native'

const props = defineProps<{ messages: Message[]; running: boolean; streaming: string; canUndo: boolean }>()
const emit = defineEmits<{ undo: [turnId: string] }>()

const turns = computed(() => {
  const history: { key: number; user?: Message; records: Message[] }[] = []
  for (const [index, message] of props.messages.entries()) {
    if (message.role === 'user') {
      history.push({ key: index, user: message, records: [] })
    } else {
      if (!history.length) history.push({ key: index, records: [] })
      history.at(-1)!.records.push(message)
    }
  }
  return history.map(({ key, user, records }) => {
    // Older transcripts only recorded message roles. A tool result anchors
    // previous assistant text to the process even without the progress flag.
    const lastTool = records.findLastIndex(message => message.role === 'tool')
    const steps: Message[] = []
    const replies: string[] = []
    const errors: Message[] = []
    for (const [index, message] of records.entries()) {
      if (message.role === 'error') errors.push(message)
      else if (message.role === 'tool') steps.push(message)
      else if (message.text.trim()) {
        if (message.progress || index < lastTool) steps.push(message)
        else replies.push(message.text)
      }
    }
    return { key, user, steps, reply: replies.join('\n\n'), errors }
  })
})

</script>

<template>
  <TransitionGroup name="message" tag="div" class="message-stack">
    <div v-for="(turn, index) in turns" :key="turn.key" class="message transcript-turn">
      <div v-if="turn.user" class="user-turn">
        <div class="user-message">
          <p>{{ turn.user.text }}</p>
          <ul v-if="turn.user.references?.length" class="reference-chips" aria-label="引用的文件">
            <li v-for="path in turn.user.references" :key="path" :title="path"><FileCode2 /><span>{{ path }}</span></li>
          </ul>
        </div>
        <span v-if="turn.user.projectRules?.length" class="turn-rules" :title="'本轮项目规则 · ' + turn.user.projectRules.join(', ')">
          <FileText /> {{ turn.user.projectRules.join(' · ') }}
        </span>
        <details v-if="turn.user.workspaceMemory" class="turn-memory">
          <summary class="turn-rules"><History /> 本轮记忆 <ChevronDown class="memory-chevron" /></summary>
          <WorkspaceMemoryList :memory="turn.user.workspaceMemory" />
        </details>
        <span v-if="turn.user.undone" class="undo-trigger muted"><CircleCheck /> 已撤回改动</span>
        <button v-else-if="turn.user.undoable && turn.user.turnId && canUndo" class="undo-trigger" type="button" @click="emit('undo', turn.user.turnId)">
          <RotateCcw /> 撤回本轮改动
        </button>
      </div>
      <TurnActivity v-if="turn.steps.length" :steps="turn.steps" :duration-ms="turn.user?.durationMs"
        :running="running && index === turns.length - 1 && turn.user?.durationMs === undefined" />
      <div v-if="turn.reply" class="agent-message">
        <header class="response-heading">
          <img class="agent-mark" src="/icon.png" alt=""><strong>BoltCode</strong>
          <div class="response-actions"><CopyButton :text="turn.reply" label="复制回复" /></div>
        </header>
        <div class="agent-response">
          <MarkdownMessage :text="turn.reply" />
        </div>
      </div>
      <p v-for="(error, errorIndex) in turn.errors" :key="errorIndex" class="failure message-error" role="status">{{ error.text }}</p>
    </div>
    <div v-if="streaming" key="stream" class="agent-message live-message" aria-label="BoltCode 正在回复">
      <header class="response-heading"><img class="agent-mark" src="/icon.png" alt=""><strong>BoltCode</strong></header>
      <p class="agent-response">{{ streaming }}</p>
    </div>
  </TransitionGroup>
</template>
