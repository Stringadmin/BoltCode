<script setup lang="ts">
import { onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { Copy, Minus, Square, X } from '@lucide/vue'
import { desktopRuntime } from './native'

const emit = defineEmits<{ error: [cause: unknown] }>()
const controller = shallowRef<Awaited<ReturnType<typeof desktopRuntime>>['Window']>()
const maximised = ref(false)
const toggling = ref(false)
let disposed = false
const unsubscribe: (() => void)[] = []

onMounted(async () => {
  try {
    const { Window, Events } = await desktopRuntime()
    if (disposed || !Window) return
    unsubscribe.push(
      Events.On('common:WindowMaximise', () => { maximised.value = true }),
      Events.On('common:WindowUnMaximise', () => { maximised.value = false }),
      Events.On('common:WindowRestore', () => { maximised.value = false }),
    )
    maximised.value = await Window.IsMaximised()
    if (!disposed) controller.value = Window
  } catch (cause) {
    if (!disposed) emit('error', cause)
  }
})

onUnmounted(() => {
  disposed = true
  unsubscribe.forEach(stop => stop())
})

async function run(action: 'Minimise' | 'Close') {
  try {
    await controller.value?.[action]()
  } catch (cause) {
    if (!disposed) emit('error', cause)
  }
}

async function toggleMaximise() {
  const window = controller.value
  if (!window || toggling.value) return
  toggling.value = true
  try {
    await window.ToggleMaximise()
    maximised.value = await window.IsMaximised()
  } catch (cause) {
    if (!disposed) emit('error', cause)
  } finally {
    toggling.value = false
  }
}

defineExpose({ toggleMaximise })
</script>

<template>
  <div class="window-controls" role="group" aria-label="窗口控制">
    <button type="button" class="caption-button" aria-label="最小化" title="最小化" :disabled="!controller" @click="run('Minimise')">
      <Minus aria-hidden="true" />
    </button>
    <button type="button" class="caption-button" :aria-label="maximised ? '还原窗口' : '最大化'"
      :title="maximised ? '还原窗口' : '最大化'" :disabled="!controller || toggling" @click="toggleMaximise">
      <Copy v-if="maximised" aria-hidden="true" /><Square v-else aria-hidden="true" />
    </button>
    <button type="button" class="caption-button caption-close" aria-label="关闭窗口" title="关闭窗口" :disabled="!controller" @click="run('Close')">
      <X aria-hidden="true" />
    </button>
  </div>
</template>
