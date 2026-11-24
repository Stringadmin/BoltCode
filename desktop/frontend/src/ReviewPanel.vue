<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { CircleCheck, FileCode2, Files, GitBranch, GitMerge, LoaderCircle, Maximize2, Minimize2, RefreshCw, X } from '@lucide/vue'
import CopyButton from './CopyButton.vue'
import VerificationBar from './VerificationBar.vue'
import { native, type ProjectChecks, type Review, type ReviewFile, type ReviewOpening, type Verification } from './native'

const props = defineProps<{ chatId: string; open: boolean; merged: boolean; archived: boolean; expanded: boolean; projectChecks: ProjectChecks }>()
const emit = defineEmits<{ close: []; merged: []; expand: []; configure: [] }>()
const review = ref<Review>()
const selected = ref('')
const diff = ref('')
const loading = ref(true)
const validated = ref(false)
const loadingFile = ref(false)
const merging = ref(false)
const confirming = ref(false)
const error = ref('')
const verification = ref<Verification>({ checks: [], running: false, changed: false })
const checkError = ref('')
const patch = ref<HTMLElement>()
const panel = ref<HTMLElement>()
let version = 0
let fileVersion = 0
let checkVersion = 0
let checkingStatus = false
let pollTimer: ReturnType<typeof setInterval> | undefined

const selectedFile = computed(() => review.value?.files.find((file) => file.path === selected.value))
const canApply = computed(() => validated.value && !loading.value && !verification.value.running && !verification.value.changed)
const totals = computed(() => (review.value?.files || []).reduce((counts, file) => ({
  added: counts.added + file.added,
  deleted: counts.deleted + file.deleted,
}), { added: 0, deleted: 0 }))

interface DiffLine {
  text: string
  kind: 'meta' | 'hunk' | 'added' | 'removed' | 'context'
  before?: number
  after?: number
}

const lines = computed<DiffLine[]>(() => {
  const input = diff.value.replace(/\r\n/g, '\n').split('\n')
  if (input.at(-1) === '') input.pop()
  const hasHunks = input.some((line) => line.startsWith('@@'))
  const output: DiffLine[] = []
  let before = 0
  let after = 0
  let inHunk = false
  for (const line of input) {
    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line)
    if (hunk) {
      before = Number(hunk[1])
      after = Number(hunk[2])
      inHunk = true
      output.push({ kind: 'hunk', text: line })
    } else if (inHunk && line.startsWith('+')) {
      output.push({ kind: 'added', text: line.slice(1), after: after++ })
    } else if (inHunk && line.startsWith('-')) {
      output.push({ kind: 'removed', text: line.slice(1), before: before++ })
    } else if (inHunk && line.startsWith(' ')) {
      output.push({ kind: 'context', text: line.slice(1), before: before++, after: after++ })
    } else if (!hasHunks || inHunk) {
      output.push({ kind: 'meta', text: line })
    }
  }
  return output
})

function filename(path: string) { return path.split('/').at(-1) || path }
function folder(path: string) { return path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '' }
function report(cause: unknown) { error.value = cause instanceof Error ? cause.message : String(cause) }

async function scan() {
  if (verification.value.running) return
  const generation = ++version
  loading.value = true
  loadingFile.value = false
  validated.value = false
  confirming.value = false
  error.value = ''
  checkError.value = ''
  try {
    const snapshot = await native<ReviewOpening>('OpenReview', props.chatId, selected.value)
    if (generation !== version) return
    const changedPatch = review.value?.token !== snapshot.token || selected.value !== snapshot.selected
    review.value = snapshot
    verification.value = snapshot.verification || { checks: [], running: false, changed: false }
    selected.value = snapshot.selected
    diff.value = snapshot.patch
    error.value = snapshot.patchError || ''
    validated.value = true
    loadingFile.value = false
    loading.value = false
    if (changedPatch) await resetPatchScroll()
  } catch (cause) {
    if (generation === version) report(cause)
  } finally {
    if (generation === version) loading.value = false
  }
}

async function updateChecks() {
  const token = review.value?.token
  if (!props.open || !token || !validated.value || loading.value || checkingStatus || props.merged || merging.value) return
  const generation = checkVersion
  checkingStatus = true
  try {
    const next = await native<Verification>('CheckStatus', props.chatId, token)
    if (token === review.value?.token && generation === checkVersion) verification.value = next
  } catch (cause) {
    if (token === review.value?.token && generation === checkVersion) {
      checkError.value = cause instanceof Error ? cause.message : String(cause)
    }
  } finally {
    checkingStatus = false
  }
}

async function runChecks() {
  const token = review.value?.token
  if (!token || merging.value || !canApply.value) return
  checkError.value = ''
  confirming.value = false
  const generation = ++checkVersion
  try {
    verification.value = { ...verification.value, running: true }
    const next = await native<Verification>('RunChecks', props.chatId, token)
    if (generation === checkVersion && token === review.value?.token) verification.value = next
  } catch (cause) {
    if (generation !== checkVersion || token !== review.value?.token) return
    verification.value = { ...verification.value, running: false }
    checkError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (generation === checkVersion) {
      checkVersion++
      if (checkError.value) void updateChecks()
    }
  }
}

async function stopChecks() {
  try {
    await native('StopChecks', props.chatId)
  } catch (cause) {
    checkError.value = cause instanceof Error ? cause.message : String(cause)
  }
}

async function navigate(file: ReviewFile) {
  const snapshot = review.value
  if (!snapshot || !validated.value || loading.value || (file.path === selected.value && !error.value)) return
  const generation = version
  const request = ++fileVersion
  selected.value = file.path
  confirming.value = false
  error.value = ''
  diff.value = ''
  loadingFile.value = true
  try {
    const next = await native<string>('Diff', props.chatId, snapshot.token, file.path)
    if (generation !== version || request !== fileVersion) return
    diff.value = next
  } catch (cause) {
    if (generation === version && request === fileVersion) report(cause)
  } finally {
    if (generation === version && request === fileVersion) {
      loadingFile.value = false
      await resetPatchScroll()
    }
  }
}

async function resetPatchScroll() {
  await nextTick()
  patch.value?.scrollTo(0, 0)
}

async function merge() {
  if (!review.value?.token || merging.value || !canApply.value) return
  merging.value = true
  confirming.value = false
  error.value = ''
  try {
    await native('Merge', props.chatId, review.value.token)
    emit('merged')
  } catch (cause) {
    report(cause)
    emit('merged')
  } finally {
    merging.value = false
  }
}

async function onOpen() {
  await nextTick()
  if (!props.open) return
  panel.value?.focus({ preventScroll: true })
  if (verification.value.running) await updateChecks()
  if (props.open) void scan()
}

function checkWhileVisible() {
  if (props.open && !document.hidden && (verification.value.running || verification.value.checks.some(check => check.result))) void updateChecks()
}

watch([() => props.projectChecks?.test, () => props.projectChecks?.typecheck], () => { void updateChecks() })
watch(() => props.open, (open) => { if (open) void onOpen() })
onMounted(() => {
  void onOpen()
  window.addEventListener('focus', checkWhileVisible)
  pollTimer = setInterval(checkWhileVisible, 4000)
})
onUnmounted(() => {
  version++
  checkVersion++
  window.removeEventListener('focus', checkWhileVisible)
  clearInterval(pollTimer)
})
</script>

<template>
  <aside id="changes-drawer" ref="panel" class="review" aria-label="改动审查" :aria-busy="loading" :inert="!open" tabindex="-1">
    <header class="review-heading">
      <h2><Files /> 改动审查 <small v-if="loading && review" class="review-refreshing" role="status">正在核对…</small></h2>
      <div class="review-heading-actions">
        <button class="quiet-icon" type="button" title="重新扫描" aria-label="重新扫描" :disabled="loading || merging || verification.running" @click="scan"><RefreshCw :class="{ spinning: loading }" /></button>
        <button class="quiet-icon" type="button" :title="expanded ? '恢复分栏' : '放大审查'" :aria-label="expanded ? '恢复分栏' : '放大审查'"
          :aria-pressed="expanded" @click="emit('expand')"><Minimize2 v-if="expanded" /><Maximize2 v-else /></button>
        <button class="quiet-icon" type="button" aria-label="关闭改动审查" title="关闭" :disabled="merging" @click="emit('close')"><X /></button>
      </div>
    </header>
    <p v-if="error" class="review-error failure" role="alert">{{ error }}</p>
    <p v-if="loading && !review" class="muted review-empty busy-label" role="status"><LoaderCircle class="spinning" />正在读取改动…</p>
    <template v-else-if="review">
      <p v-if="!review.files.length" class="muted review-empty" role="status">没有文件改动。</p>
      <div v-else class="review-content">
        <div class="review-files">
          <div class="review-summary">
            <span>{{ review.files.length }} 个文件</span>
            <span class="success">+{{ totals.added }}</span><span class="failure">−{{ totals.deleted }}</span>
            <span v-if="totals.added || totals.deleted" class="delta-meter" aria-hidden="true">
              <i v-if="totals.added" class="delta-added" :style="{ flexGrow: totals.added }"></i>
              <i v-if="totals.deleted" class="delta-removed" :style="{ flexGrow: totals.deleted }"></i>
            </span>
          </div>
          <nav class="file-list" aria-label="改动文件">
            <button v-for="file in review.files" :key="file.path" type="button" :aria-pressed="file.path === selected"
              :title="file.path" :disabled="merging || loading || !validated" @click="navigate(file)">
              <FileCode2 /><span class="file-label"><strong>{{ filename(file.path) }}</strong><small v-if="folder(file.path)">{{ folder(file.path) }}</small></span>
              <span v-if="file.binary" class="muted">二进制</span>
              <span v-else class="file-count"><span v-if="file.added" class="success">+{{ file.added }}</span><span v-if="file.deleted" class="failure">−{{ file.deleted }}</span></span>
            </button>
          </nav>
        </div>
        <section class="patch-shell" aria-label="文件差异">
          <header>
            <span :title="selected">{{ selectedFile?.path }}</span>
            <CopyButton :text="diff" label="复制 Diff" />
          </header>
          <p v-if="loadingFile" class="muted review-empty busy-label" role="status"><LoaderCircle class="spinning" />正在加载…</p>
          <div v-else ref="patch" class="patch" aria-label="代码差异" tabindex="0">
            <div class="patch-lines">
              <div v-for="(line, index) in lines" :key="index" class="diff-line" :class="line.kind">
                <span aria-hidden="true">{{ line.before }}</span><span aria-hidden="true">{{ line.after }}</span>
                <span aria-hidden="true">{{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : '' }}</span><code>{{ line.text }}</code>
              </div>
            </div>
          </div>
        </section>
      </div>
      <VerificationBar v-if="review.token && !merged && !archived" :status="verification" :error="checkError" :disabled="merging || loading || !validated"
        @run="runChecks" @stop="stopChecks" @configure="emit('configure')" @refresh="scan" />
      <footer v-if="review.files.length && !archived" class="review-actions">
        <p v-if="merged" class="success" role="status"><CircleCheck /> 已合并到 {{ review.base }}</p>
        <template v-else-if="confirming">
          <p>合并 {{ review.files.length }} 个文件到 {{ review.base }}？</p>
          <div class="actions">
            <button class="action outline" type="button" @click="confirming = false">返回</button>
            <button class="action primary" type="button" :disabled="!canApply" @click="merge">确认合并</button>
          </div>
        </template>
        <template v-else>
          <span class="review-target" :title="'目标分支 ' + review.base"><GitBranch /> <code>{{ review.base }}</code></span>
          <button class="action primary" type="button" :disabled="merging || loadingFile || !!error || !canApply" @click="confirming = true">
            <GitMerge />{{ merging ? '正在合并…' : '合并到 ' + review.base }}
          </button>
        </template>
      </footer>
    </template>
  </aside>
</template>
