<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { Cable, FolderOpen, Palette, Settings2, X } from '@lucide/vue'
import AppearanceSettings from './AppearanceSettings.vue'
import ModelSettings from './ModelSettings.vue'
import ProjectSettings from './ProjectSettings.vue'
import type { Appearance } from './appearance'
import type { State } from './native'

defineProps<{ state: State; rules: string[]; appearance: Appearance }>()
const emit = defineEmits<{ saved: []; access: []; archive: [path: string]; appearance: [value: Appearance] }>()
const tabs = [
  { id: 'models', label: '模型与渠道', icon: Cable },
  { id: 'project', label: '项目', icon: FolderOpen },
  { id: 'appearance', label: '外观', icon: Palette },
] as const
type Tab = typeof tabs[number]['id']
const active = ref<Tab>('models')
const dialog = ref<HTMLDialogElement>()
const visible = ref(false)
const body = ref<HTMLElement>()

function switchTab(tab: Tab) {
  active.value = tab
  if (body.value) body.value.scrollTop = 0
}

async function open(tab?: Tab) {
  if (tab) switchTab(tab)
  if (dialog.value?.open) return
  visible.value = true
  await nextTick()
  if (dialog.value && !dialog.value.open) dialog.value.showModal()
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="settings-dialog settings-hub" aria-labelledby="settings-title" @close="visible = false"
    @keydown.ctrl.n.stop @keydown.ctrl.o.stop>
    <div v-if="visible" class="settings-layout">
      <header class="settings-top">
        <h1 id="settings-title"><Settings2 /> 设置</h1>
        <button class="quiet-icon" type="button" aria-label="关闭设置" title="关闭设置" autofocus @click="dialog?.close()"><X /></button>
      </header>
      <nav class="settings-nav" aria-label="设置分类">
        <button v-for="tab in tabs" :key="tab.id" type="button" :aria-current="active === tab.id ? 'page' : undefined"
          @click="switchTab(tab.id)"><component :is="tab.icon" /> <span>{{ tab.label }}</span></button>
      </nav>
      <div ref="body" class="settings-body">
        <ModelSettings v-if="active === 'models'" :active="state.model" :models="state.models" :channels="state.modelChannels" :providers="state.providers" @saved="emit('saved')" />
        <ProjectSettings v-else-if="active === 'project'" :state="state" :rules="rules" @saved="emit('saved')" @access="emit('access')" @archive="emit('archive', $event)" />
        <AppearanceSettings v-else :appearance="appearance" @change="emit('appearance', $event)" />
      </div>
    </div>
  </dialog>
</template>
