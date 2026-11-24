<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  Archive, ArchiveRestore, ArrowDown, ArrowUp, ArrowUpRight, AtSign, BrainCircuit, ChevronDown, ChevronRight, CircleAlert, CircleCheck, Columns2,
  FileCode2, Files, FolderOpen, GitBranch, History, Info, PanelLeftClose, PanelLeftOpen, PencilLine, Play,
  Plus, Settings2, Shield, ShieldAlert, Square, Terminal, Trash2, X,
} from '@lucide/vue'
import ChatListItem from './ChatListItem.vue'
import ChatMessages from './ChatMessages.vue'
import FileMentions from './FileMentions.vue'
import ModelPicker from './ModelPicker.vue'
import ReviewPanel from './ReviewPanel.vue'
import ReviewDivider from './ReviewDivider.vue'
import SettingsDialog from './SettingsDialog.vue'
import UndoDialog from './UndoDialog.vue'
import WindowControls from './WindowControls.vue'
import WorkspaceMemoryList from './WorkspaceMemoryList.vue'
import { applyAppearance, normalizeAppearance, type Appearance } from './appearance'
import { copyText, native, subscribe, type Approval, type Chat, type Repository, type State, type TurnEvent, type WorkspaceMemory } from './native'

const appState = ref<State>({
  repository: '', name: '', chats: [], running: '', checking: '', recent: [], archivedProjects: [], providers: [], models: [], modelChannels: [], fullAccess: false,
  projectChecks: { test: '', typecheck: '' },
  model: { id: '', label: '', provider: 'anthropic', name: '', baseURL: '', ready: false, hasKey: false },
})
const selected = ref('')
const prompt = ref('')
const references = ref<string[]>([])
const mode = ref('build')
const initializing = ref(true)
const choosing = ref(false)
const localRun = ref('')
const phase = ref('')
const toast = ref<{ text: string; kind: 'info' | 'success' | 'error' }>()
const streaming = ref('')
const approval = ref<Approval>()
const reviewOpen = ref(false)
const reviewChat = ref('')
const reviewExpanded = ref(false)
const reviewWidth = ref<number>()
const reviewResizing = ref(false)
const followLatest = ref(true)
const stopped = ref(false)
const viewportWidth = ref(window.innerWidth)
const sidebarPreference = ref('')
const scroll = ref<HTMLElement>()
const editor = ref<HTMLTextAreaElement>()
const fileMentions = ref<InstanceType<typeof FileMentions>>()
const modelPicker = ref<InstanceType<typeof ModelPicker>>()
const undoDialog = ref<InstanceType<typeof UndoDialog>>()
const settings = ref<InstanceType<typeof SettingsDialog>>()
const appearance = ref(loadAppearance())
applyAppearance(appearance.value)
const windowControls = ref<InstanceType<typeof WindowControls>>()
const accessDialog = ref<HTMLDialogElement>()
const updatingAccess = ref(false)
const switchingModel = ref(false)
const projects = ref<HTMLDetailsElement>()
const memoryMenu = ref<HTMLDetailsElement>()
const workspaceMemory = ref<WorkspaceMemory>()
const memoryError = ref('')
const renameDialog = ref<HTMLDialogElement>()
const renameTitle = ref('')
const renameError = ref('')
const renaming = ref(false)
const archivedOpen = ref(false)
const threadMenuElement = ref<HTMLElement>()
const threadMenu = ref<{ chat: Chat; x: number; y: number; anchor: HTMLElement }>()
const removalDialog = ref<HTMLDialogElement>()
const removalTarget = ref<{ kind: 'chat' | 'project'; id: string; name: string }>()
const removalError = ref('')
const deleteProjectRecords = ref(false)
const managing = ref(false)

let stopEvents: (() => void) | undefined
let fetchVersion = 0
let runQueued = false
let incoming = ''
let frame = 0
let renameID = ''
let reviewAnchor: HTMLElement | undefined
let editorObserver: ResizeObserver | undefined
let toastTimer: ReturnType<typeof setTimeout> | undefined
let memoryRequest = 0

const chat = computed(() => appState.value.chats.find((item) => item.id === selected.value))
const projectRules = computed(() => chat.value?.messages?.findLast(message => message.role === 'user')?.projectRules || [])
const modelGroups = computed(() => {
  const known = new Map(appState.value.modelChannels.map(channel => [channel.id, channel.label]))
  const groups = new Map<string, { id: string; label: string; models: State['models'] }>()
  for (const model of appState.value.models) {
    const id = model.channelId || 'other'
    let group = groups.get(id)
    if (!group) {
      group = { id, label: known.get(id) || '其他模型', models: [] }
      groups.set(id, group)
    }
    group.models.push(model)
  }
  return [...groups.values()]
})
const activeChats = computed(() => appState.value.chats.filter((item) => !item.archived))
const archivedChats = computed(() => appState.value.chats.filter((item) => item.archived))
const running = computed(() => localRun.value || appState.value.running)
const currentRunning = computed(() => running.value !== '' && running.value === selected.value)
const controlsBusy = computed(() => initializing.value || !!running.value || !!appState.value.checking || updatingAccess.value || switchingModel.value || managing.value)
const hasThread = computed(() => !!chat.value?.messages?.length || currentRunning.value)
const canContinue = computed(() => !running.value && !appState.value.checking && !chat.value?.merged && !chat.value?.archived &&
  ['failed', 'interrupted'].includes(chat.value?.runState || ''))
const activeThread = computed(() => appState.value.chats.find((item) => item.id === running.value))
const sidebarCompact = computed(() => sidebarPreference.value ? sidebarPreference.value === 'compact' : viewportWidth.value < 730)
const reviewOverlay = computed(() => reviewOpen.value && !reviewExpanded.value && viewportWidth.value <= 980)
const reviewSplit = computed(() => reviewOpen.value && !reviewExpanded.value && !reviewOverlay.value)
const sessionStatus = computed(() => {
  if (chat.value && appState.value.checking === chat.value.id) return { label: '验证中', tone: 'running' }
  if (currentRunning.value) return approval.value
    ? { label: '待确认', tone: 'attention' }
    : { label: '运行中', tone: 'running' }
  if (chat.value?.archived) return { label: '已归档', tone: 'neutral' }
  if (chat.value?.merged) return { label: '已合并', tone: 'success' }
  if (chat.value?.runState === 'interrupted') return { label: '已停止', tone: 'neutral' }
  if (chat.value?.runState === 'failed') return { label: '执行失败', tone: 'error' }
  if (chat.value?.runState === 'done') return { label: '已回复', tone: 'neutral' }
  return undefined
})

const phases: Record<string, string> = {
  Thinking: 'Agent 正在思考',
  'Writing response': '正在回复',
  'Compacting context': '正在整理上下文',
  'Retrying request': '正在重试',
  Interrupted: '已停止',
}

function report(cause: unknown) {
  showToast(cause instanceof Error ? cause.message : String(cause), 'error')
}

function showToast(text: string, kind: 'info' | 'success' | 'error' = 'info') {
  clearTimeout(toastTimer)
  toast.value = { text, kind }
  toastTimer = setTimeout(() => { toast.value = undefined }, kind === 'error' ? 4500 : 2400)
}

function readPreference(key: string): string {
  try { return localStorage.getItem('boltcode:' + key) || '' } catch { return '' }
}

function loadAppearance() {
  try { return normalizeAppearance(JSON.parse(readPreference('appearance'))) } catch { return normalizeAppearance(null) }
}

function saveAppearance(value: Appearance) {
  appearance.value = normalizeAppearance(value)
  applyAppearance(appearance.value)
  writePreference('appearance', JSON.stringify(appearance.value))
}

function writePreference(key: string, value: string) {
  try {
    if (value) localStorage.setItem('boltcode:' + key, value)
    else localStorage.removeItem('boltcode:' + key)
  } catch { /* The in-memory draft remains editable if storage is unavailable. */ }
}

function saveReviewWidth() {
  writePreference('review-width', reviewWidth.value === undefined ? '' : String(reviewWidth.value))
}

function updateViewport() {
  viewportWidth.value = window.innerWidth
  closeThreadMenu()
}

function toggleSidebar() {
  sidebarPreference.value = sidebarCompact.value ? 'expanded' : 'compact'
  writePreference('sidebar', sidebarPreference.value)
}

function draftKey(repo = appState.value.repository, id = selected.value) {
  return 'draft:' + repo + '|' + id
}

function rememberDraft(event?: Event) {
  const value = event ? (event.target as HTMLTextAreaElement).value : prompt.value
  writePreference(draftKey(), value)
}

function rememberReferences(key = draftKey()) {
  writePreference('refs:' + key, references.value.length ? JSON.stringify(references.value) : '')
}

function restoreReferences(key: string): string[] {
  try {
    const value: unknown = JSON.parse(readPreference('refs:' + key))
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string').slice(0, 6) : []
  } catch {
    return []
  }
}

function onEditorInput(event: Event) {
  rememberDraft(event)
  if (!(event as InputEvent).isComposing) fileMentions.value?.update()
}

function insertReferenceTrigger() {
  const input = editor.value
  if (!input) return
  const start = input.selectionStart
  const prefix = start > 0 && !/\s/.test(input.value[start - 1]) ? ' ' : ''
  input.setRangeText(prefix + '@', start, input.selectionEnd, 'end')
  prompt.value = input.value
  rememberDraft()
  input.focus()
  fileMentions.value?.update()
}

function addReference(path: string, start: number, end: number) {
  const input = editor.value
  if (!input || references.value.includes(path) || references.value.length >= 6) return
  input.setRangeText('', start, end, 'end')
  prompt.value = input.value
  references.value = [...references.value, path]
  rememberDraft()
  rememberReferences()
  input.focus()
}

function removeReference(path: string) {
  references.value = references.value.filter(item => item !== path)
  rememberReferences()
  editor.value?.focus()
}

function previewUndo(turn: string) {
  if (chat.value && !chat.value.merged && !chat.value.archived && !running.value && !appState.value.checking) void undoDialog.value?.show(chat.value.id, turn)
}

async function undoRestored() {
  closeReview()
  try {
    await refresh()
    showToast('已撤回本轮工作区改动', 'success')
  } catch (cause) {
    report(cause)
  }
}

async function refresh() {
  const version = ++fetchVersion
  const next = await native<State>('State')
  if (version !== fetchVersion) return
  const changedRepository = next.repository !== appState.value.repository
  if (changedRepository) archivedOpen.value = false
  appState.value = next
  if (changedRepository || !next.chats.some((item) => item.id === selected.value)) {
    const remembered = readPreference('selected:' + next.repository)
    const target = next.chats.find((item) => item.id === remembered)?.id || activeChats.value[0]?.id || ''
    selectChat(target)
  }
  if (!localRun.value || next.approval) approval.value = next.approval
}

function selectChat(id: string) {
  closeThreadMenu()
  selected.value = id
  if (chat.value?.archived) archivedOpen.value = true
  writePreference('selected:' + appState.value.repository, id)
  prompt.value = readPreference(draftKey())
  references.value = restoreReferences(draftKey())
  fileMentions.value?.dismiss()
  modelPicker.value?.close()
  mode.value = chat.value?.runMode || 'build'
  followLatest.value = true
  closeReview()
  void nextTick(jumpLatest)
}

async function chooseRepository(path?: string) {
  if (choosing.value || running.value || appState.value.checking || managing.value || updatingAccess.value ||
    removalDialog.value?.open || accessDialog.value?.open) return
  if (projects.value) projects.value.open = false
  choosing.value = true
  try {
    if (path) await native('OpenRecent', path)
    else if (!await native<boolean>('ChooseRepository')) {
      showToast('已取消选择仓库')
      return
    }
    await refresh()
  } catch (cause) {
    report(cause)
  } finally {
    choosing.value = false
  }
}

async function createChat() {
  if (!appState.value.repository) {
    await chooseRepository()
    if (!appState.value.repository) return
  }
  const latest = activeChats.value[0]
  if (latest && !latest.messages?.length && latest.id !== running.value) {
    selectChat(latest.id)
    return
  }
  const entry = await native<Chat>('NewChat')
  appState.value.chats.unshift(entry)
  selectChat(entry.id)
}

async function newChat() {
  if (managing.value || removalDialog.value?.open) return
  try {
    await createChat()
    await nextTick()
    editor.value?.focus()
  } catch (cause) {
    report(cause)
  }
}

function openSettings(tab?: 'models' | 'project') {
  if (!controlsBusy.value) void settings.value?.open(tab)
}

function openProjectSettings() {
  if (controlsBusy.value) return
  if (projects.value) projects.value.open = false
  openSettings('project')
}

async function archiveProject(path: string) {
  if (controlsBusy.value || choosing.value || runQueued) return
  managing.value = true
  try {
    await native('ArchiveProject', path)
    if (projects.value) projects.value.open = false
    await refresh()
    showToast('已归档项目', 'success')
  } catch (cause) {
    report(cause)
  } finally {
    managing.value = false
  }
}

async function changeModel(id: string) {
  if (!id || id === appState.value.model.id || controlsBusy.value) return
  switchingModel.value = true
  try {
    await native('SelectModel', id)
    await refresh()
  } catch (cause) {
    report(cause)
  } finally {
    switchingModel.value = false
  }
}

function modelPickerOpened() {
  fileMentions.value?.dismiss()
  if (memoryMenu.value) memoryMenu.value.open = false
}

async function changeReasoningEffort(event: Event) {
  const select = event.target as HTMLSelectElement
  const effort = select.value
  const previous = appState.value.model.reasoningEffort || 'auto'
  select.value = previous
  const model = appState.value.model
  if (!model.ready || controlsBusy.value || effort === previous) return
  switchingModel.value = true
  try {
    await native('SetModelReasoningEffort', model.id, effort)
    await refresh()
  } catch (cause) {
    report(cause)
  } finally {
    switchingModel.value = false
  }
}

async function modelSaved() {
  try {
    await refresh()
  } catch (cause) {
    report(cause)
  }
}

function toggleAccess() {
  if (controlsBusy.value || choosing.value || !appState.value.repository) return
  if (appState.value.fullAccess) void setAccess(false)
  else if (!accessDialog.value?.open) accessDialog.value?.showModal()
}

async function setAccess(enabled: boolean) {
  const repository = appState.value.repository
  if (!repository || controlsBusy.value || choosing.value || (enabled && !accessDialog.value?.open)) return
  updatingAccess.value = true
  try {
    await native('SetFullAccess', repository, enabled)
    if (appState.value.repository === repository) appState.value.fullAccess = enabled
    accessDialog.value?.close()
  } catch (cause) {
    accessDialog.value?.close()
    report(cause)
  } finally {
    updatingAccess.value = false
  }
}

async function submit(continuation?: string) {
  const text = continuation || prompt.value.trim()
  if (!text || controlsBusy.value || runQueued || chat.value?.merged || chat.value?.archived) return
  runQueued = true
  const oldDraft = draftKey()
  const requestedFiles = continuation ? [] : [...references.value]
  try {
    if (!appState.value.repository) await chooseRepository()
    if (!appState.value.repository) return
    if (!appState.value.model.ready) {
      if (!continuation && !prompt.value) {
        prompt.value = text
        rememberDraft()
        if (oldDraft !== draftKey()) writePreference(oldDraft, '')
      }
      openSettings('models')
      return
    }
    if (!chat.value) await createChat()
    const entry = chat.value
    if (!entry) return
    const previousCount = entry.messages?.length || 0
    closeReview()
    fileMentions.value?.dismiss()
    if (!continuation) {
      prompt.value = ''
      references.value = []
      rememberDraft()
      rememberReferences()
      writePreference(oldDraft, '')
      writePreference('refs:' + oldDraft, '')
    }
    localRun.value = entry.id
    stopped.value = false
    followLatest.value = true
    if (!entry.messages?.length && entry.title === '新对话') entry.title = [...text.split(/\s+/).join(' ')].slice(0, 30).join('')
    ;(entry.messages ??= []).push({ role: 'user', text, references: requestedFiles })
    phase.value = entry.branch ? phases.Thinking : '正在初始化工作区'
    try {
      await native('SendWithReferences', entry.id, text, mode.value, requestedFiles)
    } catch (cause) {
      await refresh()
      const updated = appState.value.chats.find((item) => item.id === entry.id)
      if (updated?.messages?.at(-1)?.role !== 'error') report(cause)
      if (!continuation && selected.value === entry.id && (updated?.messages?.length || 0) <= previousCount) {
        prompt.value = text
        references.value = requestedFiles
        rememberDraft()
        rememberReferences()
      }
    } finally {
      localRun.value = ''
      phase.value = ''
      resetStream()
      approval.value = undefined
      await refresh()
    }
  } catch (cause) {
    report(cause)
  } finally {
    runQueued = false
  }
}

async function stop() {
  if (!running.value || stopped.value) return
  stopped.value = true
  phase.value = '正在停止'
  try {
    await native('Stop', running.value)
  } catch (cause) {
    stopped.value = false
    report(cause)
  }
}

function queueDelta(text: string) {
  incoming += text
  if (!frame) frame = requestAnimationFrame(() => {
    streaming.value += incoming
    incoming = ''
    frame = 0
  })
}

function resetStream() {
  if (frame) cancelAnimationFrame(frame)
  frame = 0
  incoming = ''
  streaming.value = ''
}

function onTurn(event: TurnEvent) {
  const entry = appState.value.chats.find((item) => item.id === event.chatId)
  if (!entry) return
  switch (event.kind) {
    case 'checks_started':
      appState.value.checking = event.chatId
      break
    case 'checks_finished':
      if (appState.value.checking === event.chatId) appState.value.checking = ''
      break
    case 'started':
      appState.value.running = event.chatId
      const message = entry.messages?.findLast(item => item.role === 'user')
      if (message) {
        message.projectRules = event.projectRules
        message.workspaceMemory = event.workspaceMemory
      }
      break
    case 'finished':
      appState.value.running = ''
      phase.value = ''
      const completed = entry.messages?.findLast(item => item.role === 'user')
      if (completed) completed.durationMs = event.durationMs
      resetStream()
      approval.value = undefined
      if (!localRun.value) void refresh().catch(report)
      break
    case 'phase':
      phase.value = phases[event.text || ''] || 'Agent 执行中'
      break
    case 'text_delta':
      queueDelta(event.text || '')
      break
    case 'text_reset':
      resetStream()
      break
    case 'text':
      resetStream()
      ;(entry.messages ??= []).push({ role: 'assistant', text: event.text || '', progress: event.progress })
      break
    case 'tool_started':
      phase.value = event.detail || 'Agent 执行中'
      break
    case 'tool_finished':
      ;(entry.messages ??= []).push({ role: 'tool', text: event.text || '', detail: event.detail, failed: event.failed, output: event.output })
      break
    case 'confirm':
      approval.value = event.prompt
      break
    case 'confirmed':
      if (approval.value?.id === event.prompt?.id) approval.value = undefined
      break
  }
}

async function answerApproval(allowed: boolean) {
  const id = approval.value?.id
  if (!id) return
  try {
    await native('AnswerApproval', id, allowed)
    if (approval.value?.id === id) approval.value = undefined
  } catch (cause) {
    report(cause)
  }
}

function closeReview(restoreFocus = false) {
  const focusedBeforeReview = reviewAnchor
  reviewOpen.value = false
  reviewExpanded.value = false
  reviewAnchor = undefined
  if (restoreFocus && focusedBeforeReview) {
    void nextTick(() => {
      if (!reviewOpen.value && focusedBeforeReview.isConnected) focusedBeforeReview.focus({ preventScroll: true })
    })
  }
}

async function loadWorkspaceMemory() {
  const menu = memoryMenu.value
  const id = selected.value
  const request = ++memoryRequest
  workspaceMemory.value = undefined
  memoryError.value = ''
  if (!menu?.open || !id) return
  fileMentions.value?.dismiss()
  try {
    const data = await native<WorkspaceMemory>('ReadWorkspaceMemory', id)
    if (memoryMenu.value === menu && menu.open && selected.value === id && request === memoryRequest) workspaceMemory.value = data
  } catch {
    if (memoryMenu.value === menu && menu.open && selected.value === id && request === memoryRequest) memoryError.value = '无法读取工作区记忆'
  }
}

async function openThreadMenu(event: MouseEvent | KeyboardEvent, item: Chat) {
  if (controlsBusy.value || choosing.value || runQueued) return
  const anchor = event.currentTarget
  if (!(anchor instanceof HTMLElement)) return
  const rect = anchor.getBoundingClientRect()
  const pointer = event instanceof MouseEvent && (event.clientX !== 0 || event.clientY !== 0)
  const x = pointer ? event.clientX : rect.left + 10
  const y = pointer ? event.clientY : rect.bottom + 3
  threadMenu.value = { chat: item, x, y, anchor }
  await nextTick()
  const menu = threadMenuElement.value
  const current = threadMenu.value
  if (!menu || !current || current.anchor !== anchor) return
  current.x = Math.max(6, Math.min(x, window.innerWidth - menu.offsetWidth - 6))
  current.y = Math.max(6, Math.min(y, window.innerHeight - menu.offsetHeight - 6))
  menu.querySelector<HTMLElement>('button')?.focus()
}

function closeThreadMenu(restoreFocus = false) {
  const anchor = threadMenu.value?.anchor
  threadMenu.value = undefined
  if (restoreFocus) anchor?.focus()
}

function threadMenuKey(event: KeyboardEvent) {
  const items = Array.from(threadMenuElement.value?.querySelectorAll('button:not(:disabled)') || [])
  const current = items.indexOf(document.activeElement as HTMLButtonElement)
  switch (event.key) {
    case 'ArrowDown':
    case 'ArrowUp':
      event.preventDefault()
      items.at((current + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length)?.focus()
      break
    case 'Tab':
      closeThreadMenu(true)
      break
  }
}

async function setArchived(item: Chat, archived: boolean) {
  if (controlsBusy.value || choosing.value || runQueued) return
  closeThreadMenu()
  managing.value = true
  try {
    await native('SetChatArchived', item.id, archived)
    await refresh()
    if (archived && selected.value === item.id) selectChat(activeChats.value[0]?.id || '')
    showToast(archived ? '已归档对话' : '已移出归档', 'success')
  } catch (cause) {
    report(cause)
  } finally {
    managing.value = false
  }
}

function confirmChatRemoval(item: Chat) {
  closeThreadMenu(true)
  void showRemovalDialog({ kind: 'chat', id: item.id, name: item.title })
}

function confirmProjectRemoval(item: Repository) {
  void showRemovalDialog({ kind: 'project', id: item.path, name: item.name })
}

async function showRemovalDialog(target: NonNullable<typeof removalTarget.value>) {
  removalError.value = ''
  deleteProjectRecords.value = false
  removalTarget.value = target
  await nextTick()
  removalDialog.value?.showModal()
}

function clearProjectDrafts(repository: string) {
  try {
    const key = draftKey(repository, '')
    const prefixes = ['boltcode:' + key, 'boltcode:refs:' + key]
    for (const key of Object.keys(localStorage)) {
      if (prefixes.some((prefix) => key.startsWith(prefix))) localStorage.removeItem(key)
    }
    writePreference('selected:' + repository, '')
  } catch { /* The native records are already removed. */ }
}

async function removeRecords() {
  const target = removalTarget.value
  if (!target || controlsBusy.value || choosing.value || runQueued) return
  removalError.value = ''
  managing.value = true
  try {
    if (target.kind === 'chat') {
      const key = draftKey(appState.value.repository, target.id)
      await native('DeleteChat', target.id)
      writePreference(key, '')
      writePreference('refs:' + key, '')
    } else {
      const deleteRecords = deleteProjectRecords.value
      await native('RemoveProject', target.id, deleteRecords)
      if (deleteRecords) clearProjectDrafts(target.id)
      if (projects.value) projects.value.open = false
    }
    removalDialog.value?.close()
    await refresh()
    showToast(target.kind === 'chat' ? '已删除对话' : '已移除项目', 'success')
  } catch (cause) {
    if (removalDialog.value?.open) removalError.value = cause instanceof Error ? cause.message : String(cause)
    else report(cause)
  } finally {
    managing.value = false
  }
}

function dismissMenus(event: PointerEvent) {
  if (threadMenu.value && !threadMenuElement.value?.contains(event.target as Node)) closeThreadMenu()
  if (projects.value?.open && !projects.value.contains(event.target as Node)) projects.value.open = false
  if (memoryMenu.value?.open && !memoryMenu.value.contains(event.target as Node)) memoryMenu.value.open = false
}

function closeOverlay(event: KeyboardEvent) {
  if (event.defaultPrevented || document.querySelector('dialog[open]')) return
  if (CSS.supports('selector(select:open)') && document.querySelector('.menu-select:open')) return
  if (threadMenu.value) closeThreadMenu(true)
  else if (memoryMenu.value?.open) {
    memoryMenu.value.open = false
    memoryMenu.value.querySelector('summary')?.focus()
  } else if (projects.value?.open) projects.value.open = false
  else if (reviewExpanded.value) reviewExpanded.value = false
  else closeReview(true)
}

function openReview(event?: MouseEvent) {
  if (!chat.value || running.value) return
  const trigger = event?.currentTarget
  reviewAnchor = trigger instanceof HTMLElement ? trigger : undefined
  reviewChat.value = chat.value.id
  reviewOpen.value = true
}

async function copyWorkspace() {
  if (!chat.value) return
  try {
    await copyText(await native<string>('WorktreePath', chat.value.id))
    showToast('已复制工作区路径', 'success')
  } catch (cause) {
    report(cause)
  }
}

function openRename() {
  if (!chat.value) return
  renameID = chat.value.id
  renameTitle.value = chat.value.title
  renameError.value = ''
  renameDialog.value?.showModal()
}

async function saveRename() {
  renaming.value = true
  renameError.value = ''
  try {
    await native('RenameChat', renameID, renameTitle.value)
    renameDialog.value?.close()
    await refresh()
  } catch (cause) {
    renameError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    renaming.value = false
  }
}

function markScroll() {
  const region = scroll.value
  if (region) followLatest.value = region.scrollHeight - region.scrollTop - region.clientHeight < 96
}

function jumpLatest() {
  if (scroll.value) scroll.value.scrollTop = scroll.value.scrollHeight
  followLatest.value = true
}

function resume() {
  if (!canContinue.value) return
  void submit('请检查当前工作区的进度并继续上次未完成的任务。保留已有改动，避免重复执行已完成的操作。')
}

function editorKey(event: KeyboardEvent) {
  if (fileMentions.value?.handleKey(event)) return
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== 229) {
    event.preventDefault()
    void submit()
  }
}

function resizeEditor() {
  const element = editor.value
  if (!element) return
  element.style.height = '0px'
  element.style.height = Math.min(176, Math.max(48, element.scrollHeight)) + 'px'
}

watch(prompt, resizeEditor, { flush: 'post' })
watch(selected, () => {
  if (memoryMenu.value) memoryMenu.value.open = false
})
watch(editor, (element) => {
  editorObserver?.disconnect()
  clearTimeout(toastTimer)
  if (!element) return
  let previousWidth = element.clientWidth
  editorObserver = new ResizeObserver(() => {
    if (element.clientWidth === previousWidth) return
    previousWidth = element.clientWidth
    resizeEditor()
  })
  editorObserver.observe(element)
  resizeEditor()
}, { flush: 'post' })

watch([() => chat.value?.messages?.length, currentRunning, phase, streaming, approval], () => {
  if (followLatest.value) jumpLatest()
}, { flush: 'post' })

onMounted(async () => {
  sidebarPreference.value = readPreference('sidebar')
  const savedWidth = Number(readPreference('review-width'))
  if (Number.isFinite(savedWidth) && savedWidth > 0) reviewWidth.value = savedWidth
  window.addEventListener('resize', updateViewport)
  try {
    stopEvents = await subscribe(onTurn)
    await refresh()
  } catch (cause) {
    report(cause)
  } finally {
    initializing.value = false
  }
})
onUnmounted(() => {
  stopEvents?.()
  resetStream()
  editorObserver?.disconnect()
  window.removeEventListener('resize', updateViewport)
})
</script>

<template>
  <div class="window" :class="{ 'compact-sidebar': sidebarCompact }" @pointerdown.capture="dismissMenus"
    @keydown.esc="closeOverlay" @keydown.ctrl.n.prevent="newChat" @keydown.ctrl.o.prevent="chooseRepository()">
    <aside class="sidebar" aria-label="对话导航">
      <div class="sidebar-header">
        <div class="brand window-drag" @dblclick="windowControls?.toggleMaximise()"><img src="/icon.png" alt="" draggable="false"><strong>BoltCode</strong></div>
        <button class="quiet-icon sidebar-toggle" type="button" :aria-expanded="!sidebarCompact"
          :aria-label="sidebarCompact ? '展开侧栏' : '收起侧栏'"
          :title="sidebarCompact ? '展开侧栏' : '收起侧栏'" @click="toggleSidebar">
          <PanelLeftOpen v-if="sidebarCompact" /><PanelLeftClose v-else />
        </button>
      </div>
      <details ref="projects" class="project-switcher">
        <summary class="repository" :title="appState.repository || '打开项目 · Ctrl+O'" aria-label="切换项目">
          <span class="repository-icon"><FolderOpen /></span>
          <span class="repository-label"><small>工作区</small><strong>{{ appState.name || '选择项目' }}</strong></span>
          <ChevronDown />
        </summary>
        <div class="project-menu">
          <button type="button" :disabled="controlsBusy || choosing" @click="chooseRepository()"><FolderOpen /> 打开 Git 仓库…</button>
          <button type="button" :disabled="controlsBusy" @click="openProjectSettings"><Settings2 /> 项目设置</button>
          <div v-if="appState.recent.length" class="project-history">
            <span>最近打开</span>
            <div v-for="item in appState.recent" :key="item.path" class="project-row">
              <button type="button" :title="item.path" :aria-pressed="item.path === appState.repository"
                :disabled="controlsBusy || choosing" @click="chooseRepository(item.path)">
                <FolderOpen /><span><strong>{{ item.name }}</strong><small>{{ item.path }}</small></span>
                <CircleCheck v-if="item.path === appState.repository" />
              </button>
              <button class="project-archive" type="button" :aria-label="'归档项目 ' + item.name" title="归档项目"
                :disabled="controlsBusy || choosing" @click="archiveProject(item.path)"><Archive /></button>
              <button class="project-remove" type="button" :aria-label="'移除项目 ' + item.name" title="移除项目"
                :disabled="controlsBusy || choosing" @click="confirmProjectRemoval(item)"><Trash2 /></button>
            </div>
          </div>
        </div>
      </details>
      <button class="action new-thread" aria-label="新对话" title="新对话 · Ctrl+N" :disabled="initializing || choosing" @click="newChat">
        <Plus /><span>新对话</span>
      </button>
      <nav class="threads" aria-label="最近对话">
        <p v-if="activeChats.length" class="section-caption">对话</p>
        <ChatListItem v-for="item in activeChats" :key="item.id" :chat="item" :selected="item.id === selected"
          :running="item.id === running" :checking="item.id === appState.checking" @select="selectChat(item.id)" @menu="openThreadMenu($event, item)" />
        <details v-if="archivedChats.length" class="archived-threads" :open="archivedOpen" @toggle="archivedOpen = ($event.currentTarget as HTMLDetailsElement).open">
          <summary aria-label="已归档对话" title="已归档对话"><Archive /><span>已归档</span><ChevronDown /></summary>
          <div>
            <ChatListItem v-for="item in archivedChats" :key="item.id" :chat="item" :selected="item.id === selected"
              :running="false" :checking="false" @select="selectChat(item.id)" @menu="openThreadMenu($event, item)" />
          </div>
        </details>
      </nav>
      <div class="side-footer">
        <button class="settings-entry action" aria-label="设置" title="设置" :disabled="controlsBusy" @click="openSettings()">
          <span class="settings-entry-icon"><Settings2 /></span>
          <strong class="settings-entry-label">设置</strong>
          <ChevronRight class="settings-entry-control" />
        </button>
      </div>
    </aside>

    <main class="main">
      <header class="topbar">
        <div class="session-heading">
          <div v-if="appState.name" class="session-path"><FolderOpen /><span>{{ appState.name }}</span><ChevronRight /><span>对话</span></div>
          <div class="session-title">
            <h1>{{ chat?.title || '新对话' }}</h1>
            <button v-if="chat" class="quiet-icon" type="button" aria-label="重命名对话" title="重命名对话" @click="openRename"><PencilLine /></button>
            <span v-if="sessionStatus" class="session-status" :data-tone="sessionStatus.tone" role="status">{{ sessionStatus.label }}</span>
          </div>
        </div>
        <div class="window-drag window-drag-space" aria-hidden="true" @dblclick="windowControls?.toggleMaximise()"></div>
        <div v-if="chat?.branch" class="session-actions">
          <button class="branch" type="button" :aria-label="'复制工作区路径 · ' + chat.branch" :title="'复制工作区路径 · ' + chat.branch" @click="copyWorkspace">
            <GitBranch /><span>{{ chat.base || '工作区' }}</span>
          </button>
          <button class="action outline review-toggle" :aria-expanded="reviewOpen" aria-controls="changes-drawer" aria-label="改动审查"
            title="改动审查" :disabled="!!running" @click="reviewOpen ? closeReview(true) : openReview($event)"><Columns2 /><span>改动</span></button>
        </div>
        <WindowControls ref="windowControls" @error="report" />
      </header>
      <div class="body" :class="{ 'with-review': reviewOpen, 'review-expanded': reviewExpanded, 'review-resizing': reviewResizing }"
        :style="{ '--preferred-review-width': reviewWidth === undefined ? undefined : reviewWidth + 'px' }">
        <div class="chat" :inert="reviewOverlay || reviewExpanded">
          <div ref="scroll" class="scroll" @scroll.passive="markScroll">
            <div class="conversation">
              <div v-if="initializing" class="empty">
                <div class="empty-mark"><img src="/icon.png" alt=""></div><h2>正在打开 BoltCode…</h2>
              </div>
              <div v-else-if="!hasThread" class="empty">
                <div class="empty-mark">
                  <FolderOpen v-if="!appState.repository" /><Settings2 v-else-if="!appState.model.ready" />
                  <img v-else src="/icon.png" alt="">
                </div>
                <h2 v-if="!appState.repository">打开一个项目</h2>
                <h2 v-else-if="!appState.model.ready">配置编程模型</h2>
                <h2 v-else>开始一条开发对话</h2>
                <p v-if="!appState.repository">从本地 Git 仓库开始。</p>
                <p v-else-if="appState.model.ready">{{ appState.name }}</p>
                <button v-if="!appState.repository" class="action primary" :disabled="choosing" @click="chooseRepository()"><FolderOpen /> 打开 Git 仓库</button>
                <button v-else-if="!appState.model.ready" class="action primary" @click="openSettings('models')"><Settings2 /> 配置模型</button>
              </div>
              <ChatMessages v-else :key="chat?.id || ''" :messages="chat?.messages || []" :running="currentRunning" :streaming="currentRunning ? streaming : ''"
                :can-undo="!running && !appState.checking && !chat?.merged && !chat?.archived" @undo="previewUndo" />
              <p v-if="currentRunning && phase && !streaming && !approval" class="thinking" role="status">
                <span class="typing-dots" aria-hidden="true"><i></i><i></i><i></i></span><span>{{ phase }}</span>
              </p>
              <div v-if="canContinue" class="resume-row">
                <span>{{ chat?.runState === 'failed' ? '上次执行失败' : '任务已停止' }}</span>
                <button class="action outline" type="button" @click="resume"><Play /> 继续任务</button>
              </div>
              <button v-if="chat?.branch && !currentRunning && !chat.merged && hasThread" class="change-card"
                :aria-expanded="reviewOpen" aria-controls="changes-drawer" :disabled="!!running" @click="openReview">
                <span class="change-icon"><Files /></span><strong>工作区改动</strong>
                <span class="change-action">查看 Diff<ChevronRight /></span>
              </button>
            </div>
          </div>
          <div class="dock">
            <Transition name="rise">
              <button v-if="!followLatest" class="action outline jump-latest" type="button" @click="jumpLatest"><ArrowDown /> 最新消息</button>
            </Transition>
            <div v-if="currentRunning && approval" class="approval">
              <div><strong><Terminal /> 允许运行？</strong><code>{{ approval.command }}</code></div>
              <div class="actions">
                <button class="action outline" @click="answerApproval(false)">拒绝</button>
                <button class="action primary" @click="answerApproval(true)">允许</button>
              </div>
            </div>
            <button v-else-if="approval && activeThread" class="action outline pending-approval" type="button" @click="selectChat(activeThread.id)">
              <Terminal /> {{ activeThread.title }} · 等待命令确认 <ArrowUpRight />
            </button>
            <div v-if="chat?.archived" class="composer archive-state"><Archive /> 已归档
              <button class="action outline" type="button" :disabled="controlsBusy" @click="setArchived(chat, false)"><ArchiveRestore /> 移出归档</button>
            </div>
            <form v-else-if="!chat?.merged" class="composer" @submit.prevent="submit()">
              <FileMentions ref="fileMentions" :editor="editor" :chat-id="selected" :repository="appState.repository" :selected="references"
                @pick="addReference" />
              <ul v-if="references.length" class="reference-chips" aria-label="本轮引用的文件">
                <li v-for="path in references" :key="path" :title="path"><FileCode2 /><span>{{ path }}</span>
                  <button type="button" :aria-label="'移除引用 ' + path" @click="removeReference(path)"><X /></button>
                </li>
              </ul>
              <textarea ref="editor" v-model="prompt" rows="2" aria-label="给 BoltCode 发消息" placeholder="描述你想做的改动…"
                :disabled="initializing" aria-autocomplete="list" aria-haspopup="listbox"
                :aria-controls="fileMentions?.open ? 'reference-options' : undefined"
                :aria-activedescendant="fileMentions?.open && fileMentions.hasOptions() ? 'reference-option-' + fileMentions.active : undefined"
                @input="onEditorInput" @click="fileMentions?.update()" @blur="fileMentions?.dismiss()"
                @keyup.left="fileMentions?.update()" @keyup.right="fileMentions?.update()" @keydown="editorKey"></textarea>
              <div class="tools">
                <label class="select-control mode"><Settings2 class="mode-icon" aria-hidden="true" /><select v-model="mode" class="menu-select" aria-label="开发模式">
                  <option value="build">构建</option><option value="plan">计划</option><option value="ask">问答</option>
                </select><ChevronDown class="select-chevron" aria-hidden="true" /></label>
                <button class="mention-toggle" type="button" aria-label="引用工作区文件" title="引用工作区文件 · @"
                  :aria-expanded="fileMentions?.open || false" :disabled="!appState.repository || initializing || references.length >= 6"
                  @click="insertReferenceTrigger"><AtSign /></button>
                <details v-if="chat?.branch && !controlsBusy" ref="memoryMenu" class="workspace-memory-menu" @toggle="loadWorkspaceMemory">
                  <summary class="mention-toggle" aria-label="工作区记忆" title="工作区记忆"><History /></summary>
                  <div class="workspace-memory-popover">
                    <strong>工作区记忆</strong>
                    <p v-if="memoryError" class="memory-empty" role="status">{{ memoryError }}</p>
                    <p v-else-if="!workspaceMemory" class="memory-empty" role="status">正在核对…</p>
                    <p v-else-if="!workspaceMemory.facts?.length && !workspaceMemory.checks?.length && !workspaceMemory.toolLessons?.length" class="memory-empty">暂无可核对的项目记忆</p>
                    <WorkspaceMemoryList v-else :memory="workspaceMemory" />
                  </div>
                </details>
                <ModelPicker v-if="appState.models.length" ref="modelPicker" :model="appState.model" :groups="modelGroups" :disabled="controlsBusy"
                  @select="changeModel" @opened="modelPickerOpened" />
                <button v-else type="button" class="model-label" :disabled="controlsBusy" @click="openSettings('models')">
                  <span>配置模型</span><ChevronDown />
                </button>
                <label v-if="appState.model.ready && appState.model.reasoningSupported" class="select-control effort-select">
                  <BrainCircuit class="mode-icon" aria-hidden="true" />
                  <select class="menu-select" aria-label="思维强度" title="思维强度" :value="appState.model.reasoningEffort || 'auto'"
                    :disabled="controlsBusy" @change="changeReasoningEffort">
                    <option value="auto">默认</option><option value="low">低</option><option value="medium">中</option>
                    <option value="high">高</option><option value="xhigh">超高</option>
                  </select><ChevronDown class="select-chevron" aria-hidden="true" />
                </label>
                <button v-if="appState.repository" class="access-toggle" type="button" :aria-pressed="appState.fullAccess"
                  :disabled="controlsBusy || choosing"
                  :title="appState.fullAccess ? '切回限制访问' : '常规命令逐条确认'" @click="toggleAccess">
                  <ShieldAlert v-if="appState.fullAccess" /><Shield v-else />
                  <span>{{ appState.fullAccess ? '完全访问' : '限制访问' }}</span>
                </button>
                <button v-if="currentRunning" class="send stop" type="button" :disabled="stopped" aria-label="停止" title="停止" @click="stop"><Square /></button>
                <button v-else class="send" type="submit" :disabled="!prompt.trim() || controlsBusy"
                  aria-label="发送消息" title="Enter 发送 · Shift+Enter 换行"><ArrowUp /></button>
              </div>
            </form>
            <div v-else class="composer resolved"><CircleCheck /> 已合并到 {{ chat.base }} <button class="action outline" @click="newChat"><Plus /> 新对话</button></div>
          </div>
        </div>

        <Transition name="veil">
          <button v-if="reviewOverlay" class="drawer-scrim" type="button" aria-label="返回对话" @click="closeReview(true)"></button>
        </Transition>
        <Transition name="drawer">
          <ReviewPanel v-if="chat && reviewChat === chat.id" v-show="reviewOpen" :key="chat.id" :open="reviewOpen"
            :chat-id="chat.id" :merged="chat.merged" :archived="!!chat.archived" :expanded="reviewExpanded"
            :project-checks="appState.projectChecks" @configure="openProjectSettings"
            @close="closeReview(true)" @expand="reviewExpanded = !reviewExpanded" @merged="refresh().catch(report)" />
        </Transition>
        <ReviewDivider v-if="reviewSplit" v-model="reviewWidth" @resizing="reviewResizing = $event" @save="saveReviewWidth" />
      </div>
    </main>
    <SettingsDialog ref="settings" :state="appState" :rules="projectRules" :appearance="appearance"
      @saved="modelSaved" @access="toggleAccess" @archive="archiveProject" @appearance="saveAppearance" />
    <UndoDialog ref="undoDialog" @restored="undoRestored" />
    <div v-if="threadMenu" ref="threadMenuElement" class="thread-menu" role="menu" :aria-label="threadMenu.chat.title"
      :style="{ left: threadMenu.x + 'px', top: threadMenu.y + 'px' }" @keydown="threadMenuKey">
      <button type="button" role="menuitem" :disabled="controlsBusy" @click="setArchived(threadMenu.chat, !threadMenu.chat.archived)">
        <ArchiveRestore v-if="threadMenu.chat.archived" /><Archive v-else />
        {{ threadMenu.chat.archived ? '移出归档' : '归档' }}
      </button>
      <button class="destructive" type="button" role="menuitem" :disabled="controlsBusy" @click="confirmChatRemoval(threadMenu.chat)">
        <Trash2 /> 删除
      </button>
    </div>
    <dialog ref="removalDialog" class="settings-dialog removal-dialog" aria-labelledby="removal-title" aria-describedby="removal-detail"
      @cancel="managing && $event.preventDefault()" @close="removalTarget = undefined">
      <form v-if="removalTarget" @submit.prevent="removeRecords">
        <h2 id="removal-title"><Trash2 /> {{ removalTarget.kind === 'chat' ? '删除对话？' : '移除项目？' }}</h2>
        <div id="removal-detail">
          <p><strong>{{ removalTarget.name }}</strong></p>
          <p v-if="removalTarget.kind === 'chat'" class="muted">对话、草稿和相关记忆会永久删除。Git 工作区文件会保留。</p>
          <p v-else class="muted">项目会从列表移除。Git 仓库和任务工作区文件会保留。</p>
        </div>
        <label v-if="removalTarget.kind === 'project'" class="removal-option">
          <input v-model="deleteProjectRecords" type="checkbox" :disabled="managing">
          同时永久删除此项目的对话、草稿和项目设置
        </label>
        <p v-if="removalError" class="failure" role="alert">{{ removalError }}</p>
        <div class="actions">
          <button class="action outline" type="button" autofocus :disabled="managing" @click="removalDialog?.close()">取消</button>
          <button class="action destructive-action" type="submit" :disabled="controlsBusy || choosing">
            {{ removalTarget.kind === 'chat' ? '删除对话' : '移除项目' }}
          </button>
        </div>
      </form>
    </dialog>
    <dialog ref="accessDialog" class="settings-dialog access-dialog" aria-labelledby="access-title" aria-describedby="access-detail"
      @cancel="updatingAccess && $event.preventDefault()">
      <form @submit.prevent="setAccess(true)">
        <h2 id="access-title"><ShieldAlert /> 启用完全访问？</h2>
        <div id="access-detail">
          <p>常规命令将以你的 Windows 用户权限直接执行，可以访问当前工作区之外的文件。</p>
          <p class="muted">此授权会为当前项目保存，重启后继续生效。已识别的高风险命令和 Git 提交仍需确认。</p>
        </div>
        <div class="actions">
          <button class="action outline" type="button" autofocus :disabled="updatingAccess" @click="accessDialog?.close()">取消</button>
          <button class="action primary" type="submit" :disabled="updatingAccess">允许完全访问</button>
        </div>
      </form>
    </dialog>
    <dialog ref="renameDialog" class="settings-dialog rename-dialog">
      <form @submit.prevent="saveRename">
        <header><h2>重命名对话</h2><button class="icon-button" type="button" aria-label="关闭" @click="renameDialog?.close()"><X /></button></header>
        <label>名称<input v-model.trim="renameTitle" required maxlength="100" autofocus></label>
        <p v-if="renameError" class="failure" role="alert">{{ renameError }}</p>
        <button class="action primary settings-save" :disabled="renaming">保存</button>
      </form>
    </dialog>
  </div>
  <div class="toast-layer" role="status" aria-atomic="true">
    <Transition name="toast">
      <div v-if="toast" class="toast" :data-kind="toast.kind">
        <CircleAlert v-if="toast.kind === 'error'" aria-hidden="true" />
        <CircleCheck v-else-if="toast.kind === 'success'" aria-hidden="true" />
        <Info v-else aria-hidden="true" />
        <span>{{ toast.text }}</span>
      </div>
    </Transition>
  </div>
</template>
