<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Cable, Check, ChevronDown, Cpu, PencilLine, Plus, Radio } from '@lucide/vue'
import { native, type Model, type ModelChannel, type Provider, type ReasoningEffort } from './native'

const props = defineProps<{ active: Model; models: Model[]; channels: ModelChannel[]; providers: Provider[] }>()
const emit = defineEmits<{ saved: [] }>()
const selectedID = ref('')
const channel = computed(() => props.channels.find(item => item.id === selectedID.value))
const models = computed(() => props.models.filter(item => item.channelId === selectedID.value))
const channelEditor = ref(false)
const modelEditor = ref(false)
const creatingChannel = ref(false)
const editingID = ref('')
const channelLabel = ref('')
const provider = ref('openai')
const url = ref('')
const key = ref('')
const label = ref('')
const modelID = ref('')
const effort = ref<ReasoningEffort>('auto')
const busy = ref('')
const error = ref('')
const status = ref('')
const catalog = computed(() => props.providers.some(item => item.id === provider.value) ? props.providers :
  [...props.providers, { id: provider.value, label: provider.value, baseURL: '', custom: true }])
const providerInfo = computed(() => catalog.value.find(item => item.id === provider.value))
const address = computed(() => (url.value.trim() || providerInfo.value?.baseURL || '').replace(/\/+$/, ''))
const storedKey = computed(() => !creatingChannel.value && channel.value?.hasKey && channel.value.provider === provider.value &&
  channel.value.baseURL.replace(/\/+$/, '') === address.value)
const localEndpoint = computed(() => {
  try {
    const host = new URL(address.value).hostname
    return host === 'localhost' || host === '[::1]' || /^127\.(?:\d+\.){2}\d+$/.test(host)
  } catch { return false }
})
const effortNames: Record<ReasoningEffort, string> = { auto: '默认', low: '低', medium: '中', high: '高', xhigh: '超高' }

watch(() => props.channels, (channels) => {
  if (!channels.some(item => item.id === selectedID.value)) {
    selectedID.value = props.active.channelId || channels[0]?.id || ''
  }
  if (!channels.length && !channelEditor.value) editChannel(true)
}, { immediate: true })

function clearFeedback() {
  error.value = ''
  status.value = ''
}

function selectChannel(id: string) {
  selectedID.value = id
  channelEditor.value = false
  modelEditor.value = false
  key.value = ''
  clearFeedback()
}

function editChannel(create: boolean) {
  creatingChannel.value = create
  channelLabel.value = create ? '' : channel.value?.label || ''
  provider.value = create ? 'openai' : channel.value?.provider || 'openai'
  url.value = create ? '' : channel.value?.baseURL || ''
  key.value = ''
  channelEditor.value = true
  modelEditor.value = false
  clearFeedback()
}

function changeProvider() {
  url.value = provider.value === channel.value?.provider && !creatingChannel.value ? channel.value.baseURL : ''
  key.value = ''
  clearFeedback()
}

function editModel(model?: Model) {
  editingID.value = model?.id || ''
  label.value = model?.label || ''
  modelID.value = model?.name || ''
  effort.value = model?.reasoningEffort || 'auto'
  modelEditor.value = true
  channelEditor.value = false
  clearFeedback()
}

async function saveChannel() {
  if (busy.value) return
  busy.value = 'channel'
  clearFeedback()
  try {
    const saved = await native<ModelChannel>('SaveModelChannel', creatingChannel.value ? '' : selectedID.value,
      channelLabel.value, provider.value, url.value, key.value)
    selectChannel(saved.id)
    emit('saved')
  } catch (cause) {
    error.value = String(cause instanceof Error ? cause.message : cause)
  } finally { busy.value = '' }
}

async function saveModel() {
  if (busy.value || !channel.value) return
  busy.value = 'model'
  clearFeedback()
  try {
    await native<Model>('SaveChannelModel', selectedID.value, editingID.value, label.value, modelID.value, effort.value)
    modelEditor.value = false
    emit('saved')
  } catch (cause) {
    error.value = String(cause instanceof Error ? cause.message : cause)
  } finally { busy.value = '' }
}

async function testModel() {
  if (busy.value || !channel.value || !modelID.value.trim()) return
  busy.value = 'test'
  clearFeedback()
  try {
    const latency = await native<number>('TestChannelModel', selectedID.value, modelID.value)
    status.value = '连接成功 · ' + latency + ' ms'
  } catch (cause) {
    error.value = String(cause instanceof Error ? cause.message : cause)
  } finally { busy.value = '' }
}

async function activate(id: string) {
  if (busy.value) return
  busy.value = 'select'
  clearFeedback()
  try {
    await native('SelectModel', id)
    emit('saved')
  } catch (cause) {
    error.value = String(cause instanceof Error ? cause.message : cause)
  } finally { busy.value = '' }
}
</script>

<template>
  <section class="settings-page channel-settings">
    <div class="settings-page-heading">
      <h2>模型与渠道</h2>
      <button class="action outline" type="button" :disabled="!!busy || (creatingChannel && channelEditor)" @click="editChannel(true)"><Plus /> 新增渠道</button>
    </div>
    <div v-if="channels.length" class="settings-section channel-switch">
      <label>渠道
        <span class="select-control settings-select">
          <select class="menu-select" aria-label="渠道" :value="selectedID" :disabled="!!busy" @change="selectChannel(($event.target as HTMLSelectElement).value)">
            <option v-for="item in channels" :key="item.id" :value="item.id">{{ item.label }}</option>
          </select>
          <ChevronDown class="select-chevron" aria-hidden="true" />
        </span>
      </label>
      <button v-if="channel" class="action outline" type="button" :disabled="!!busy" @click="editChannel(false)"><PencilLine /> 编辑渠道</button>
    </div>
    <form v-if="channelEditor" class="settings-section settings-editor" @submit.prevent="saveChannel">
      <h3>{{ creatingChannel ? '新增渠道' : '编辑渠道' }}</h3>
      <fieldset :disabled="!!busy" class="settings-fields">
        <label>渠道名称<input v-model.trim="channelLabel" required maxlength="64" placeholder="例如 RightCode" autocomplete="off"></label>
        <label>供应商与协议
          <span class="select-control settings-select"><select v-model="provider" class="menu-select" @change="changeProvider">
            <option v-for="item in catalog" :key="item.id" :value="item.id">{{ item.label }}</option>
          </select><ChevronDown class="select-chevron" aria-hidden="true" /></span>
        </label>
        <label class="settings-field-wide">API 地址
          <input v-model.trim="url" type="url" :required="!providerInfo?.baseURL" :placeholder="providerInfo?.baseURL || 'https://'" spellcheck="false" autocomplete="off">
        </label>
        <label class="settings-field-wide">API Key
          <input v-model.trim="key" type="password" autocomplete="off" :required="!storedKey && !localEndpoint"
            :placeholder="storedKey ? '留空沿用已保存密钥' : localEndpoint ? '本机服务可留空' : ''">
        </label>
      </fieldset>
      <div class="actions">
        <button v-if="channels.length" class="action outline" type="button" :disabled="!!busy" @click="selectChannel(selectedID)">取消</button>
        <button class="action primary" type="submit" :disabled="!!busy">{{ busy === 'channel' ? '正在保存…' : '保存渠道' }}</button>
      </div>
    </form>
    <template v-else-if="channel">
      <p class="settings-path"><Cable /> <span>{{ channel.baseURL }}</span></p>
      <section class="settings-section">
        <div class="settings-subheading"><h3>渠道模型</h3>
          <button class="action outline" type="button" :disabled="!!busy" @click="editModel()"><Plus /> 添加模型</button>
        </div>
        <ul v-if="models.length" class="settings-list">
          <li v-for="model in models" :key="model.id">
            <Cpu /><div><strong>{{ model.label }}</strong><small>{{ model.name }}</small></div>
            <span v-if="model.id === active.id" class="settings-current"><Check /> 当前</span>
            <button v-else class="action outline" type="button" :disabled="!!busy || !model.ready" @click="activate(model.id)">使用</button>
            <button class="quiet-icon" type="button" :aria-label="'编辑模型 ' + model.label" title="编辑模型" :disabled="!!busy" @click="editModel(model)"><PencilLine /></button>
          </li>
        </ul>
        <p v-else-if="!modelEditor" class="muted">此渠道还没有模型。</p>
      </section>
      <form v-if="modelEditor" class="settings-section settings-editor" @submit.prevent="saveModel">
        <h3>{{ editingID ? '编辑模型' : '添加模型' }}</h3>
        <fieldset :disabled="!!busy" class="settings-fields">
          <label>显示名称<input v-model.trim="label" maxlength="64" :placeholder="modelID || '可选'" autocomplete="off"></label>
          <label>思维强度
            <span class="select-control settings-select"><select v-model="effort" class="menu-select">
              <option v-for="(name, id) in effortNames" :key="id" :value="id">{{ name }}</option>
            </select><ChevronDown class="select-chevron" aria-hidden="true" /></span>
          </label>
          <label class="settings-field-wide">模型 ID<input v-model.trim="modelID" required spellcheck="false" autocomplete="off"></label>
        </fieldset>
        <div class="settings-actions">
          <button class="action outline" type="button" :disabled="!!busy || !modelID" @click="testModel"><Radio /> {{ busy === 'test' ? '测试中…' : '测试连接' }}</button>
          <div class="actions">
            <button class="action outline" type="button" :disabled="!!busy" @click="modelEditor = false; clearFeedback()">取消</button>
            <button class="action primary" type="submit" :disabled="!!busy">{{ busy === 'model' ? '正在保存…' : '保存模型' }}</button>
          </div>
        </div>
      </form>
    </template>
    <p v-if="error" class="failure" role="alert">{{ error }}</p>
    <p v-if="status" class="success" role="status">{{ status }}</p>
  </section>
</template>
