<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { FileCode2, LoaderCircle, RotateCcw, X } from '@lucide/vue'
import { native, type UndoPreview } from './native'

const emit = defineEmits<{ restored: [] }>()
const dialog = ref<HTMLDialogElement>()
const preview = ref<UndoPreview>()
const loading = ref(false)
const applying = ref(false)
const error = ref('')
let request = 0
let target: { chat: string; turn: string } | undefined

async function load() {
  if (!target) return
  const generation = ++request
  preview.value = undefined
  error.value = ''
  loading.value = true
  try {
    const result = await native<UndoPreview>('PreviewUndo', target.chat, target.turn)
    if (generation === request) preview.value = result
  } catch (cause) {
    if (generation === request) error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (generation === request) loading.value = false
  }
}

async function show(chat: string, turn: string) {
  if (!dialog.value || dialog.value.open) return
  const selection = { chat, turn }
  target = selection
  void load()
  await nextTick()
  if (target === selection && !dialog.value?.open) dialog.value?.showModal()
}

function reset() {
  if (dialog.value?.open) return
  request++
  target = undefined
}

async function apply() {
  if (!target || !preview.value?.allowed || applying.value) return
  applying.value = true
  try {
    await native('UndoTurn', target.chat, target.turn)
    dialog.value?.close()
    emit('restored')
  } catch (cause) {
    preview.value = undefined
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    applying.value = false
  }
}

defineExpose({ show })
</script>

<template>
  <dialog ref="dialog" class="settings-dialog undo-dialog" aria-labelledby="undo-title" @close="reset" @cancel="applying && $event.preventDefault()"
    @keydown.ctrl.n.stop @keydown.ctrl.o.stop>
    <form @submit.prevent="apply">
      <header><h2 id="undo-title"><RotateCcw /> 撤回本轮改动</h2>
        <button type="button" class="quiet-icon" aria-label="关闭撤回预览" :disabled="applying" @click="dialog?.close()"><X /></button>
      </header>
      <p v-if="loading" class="muted busy-label" role="status"><LoaderCircle class="spinning" /> 正在比对当前工作区…</p>
      <p v-else-if="error" class="failure" role="alert">{{ error }}</p>
      <template v-else-if="preview">
        <p v-if="preview.alreadyRestored" class="muted">这些文件已经恢复，可以确认撤回记录。</p>
        <p v-else class="muted">会恢复下列工作区文件。对话记录和 Git 提交会保留。</p>
        <ul class="undo-files">
          <li v-for="file in preview.files" :key="file.path">
            <FileCode2 /><span :title="file.path">{{ file.path }}</span>
            <span :class="{ failure: file.block }">{{ file.block || (preview.alreadyRestored ? '已恢复' : file.action) }}</span>
          </li>
        </ul>
      </template>
      <footer class="settings-actions">
        <button v-if="!loading && (!preview?.allowed || error)" type="button" class="action outline" :disabled="applying" @click="load">刷新预览</button>
        <div class="actions">
          <button class="action outline" type="button" autofocus :disabled="applying" @click="dialog?.close()">取消</button>
          <button class="action primary" :disabled="loading || !preview?.allowed || applying">{{ applying ? '正在撤回…' : preview?.alreadyRestored ? '确认已恢复' : '确认撤回' }}</button>
        </div>
      </footer>
    </form>
  </dialog>
</template>
