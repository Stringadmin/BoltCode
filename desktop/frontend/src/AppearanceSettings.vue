<script setup lang="ts">
import { Laptop, Moon, RotateCcw, Sun } from '@lucide/vue'
import { defaultAppearance, type Appearance } from './appearance'

const props = defineProps<{ appearance: Appearance }>()
const emit = defineEmits<{ change: [value: Appearance] }>()
const themes = [
  { id: 'system', label: '跟随系统', icon: Laptop }, { id: 'light', label: '浅色', icon: Sun }, { id: 'dark', label: '深色', icon: Moon },
] as const
const accents = [
  { id: 'indigo', label: '靛蓝', color: '#586cbe' }, { id: 'teal', label: '松绿', color: '#25816d' }, { id: 'berry', label: '莓紫', color: '#9d5686' },
] as const
const backgrounds = [
  { id: 'default', label: '默认' }, { id: 'paper', label: '暖纸' }, { id: 'slate', label: '冷灰' },
] as const

function update<K extends keyof Appearance>(key: K, value: Appearance[K]) {
  emit('change', { ...props.appearance, [key]: value })
}
</script>

<template>
  <section class="settings-page appearance-page">
    <div class="settings-page-heading">
      <h2>外观</h2>
      <button class="action outline" type="button" @click="emit('change', { ...defaultAppearance })"><RotateCcw /> 恢复默认</button>
    </div>
    <div class="settings-section">
      <h3>主题</h3>
      <div class="settings-segmented" role="group" aria-label="主题">
        <button v-for="theme in themes" :key="theme.id" type="button" :aria-pressed="appearance.theme === theme.id"
          @click="update('theme', theme.id)"><component :is="theme.icon" /> {{ theme.label }}</button>
      </div>
    </div>
    <div class="settings-section appearance-colors">
      <div>
        <h3>强调色</h3>
        <div class="settings-swatches" role="group" aria-label="强调色">
          <button v-for="entry in accents" :key="entry.id" type="button" :aria-pressed="appearance.accent === entry.id"
            :aria-label="entry.label" :title="entry.label" @click="update('accent', entry.id)"><i :style="{ background: entry.color }"></i></button>
        </div>
      </div>
      <div>
        <h3>背景</h3>
        <div class="settings-segmented" role="group" aria-label="背景">
          <button v-for="entry in backgrounds" :key="entry.id" type="button" :aria-pressed="appearance.background === entry.id"
            @click="update('background', entry.id)">{{ entry.label }}</button>
        </div>
      </div>
    </div>
    <div class="settings-section appearance-fonts">
      <label><span>对话字号 <output>{{ appearance.chatSize }} px</output></span>
        <input type="range" aria-label="对话字号" :value="appearance.chatSize" min="13" max="20"
          @input="update('chatSize', ($event.target as HTMLInputElement).valueAsNumber)">
      </label>
      <label><span>代码字号 <output>{{ appearance.codeSize }} px</output></span>
        <input type="range" aria-label="代码字号" :value="appearance.codeSize" min="11" max="17"
          @input="update('codeSize', ($event.target as HTMLInputElement).valueAsNumber)">
      </label>
    </div>
    <div class="appearance-preview" aria-label="外观预览">
      <span>对话预览</span>
      <p>构建完成，检查已通过。</p>
      <pre><code>go test ./...</code></pre>
    </div>
  </section>
</template>
