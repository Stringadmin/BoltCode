<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const width = defineModel<number>()
const emit = defineEmits<{ resizing: [active: boolean]; save: [] }>()
const divider = ref<HTMLElement>()
const bodyWidth = ref(0)
const minimum = 320
const maximum = computed(() => Math.max(minimum, bodyWidth.value - 320))
const defaultWidth = computed(() => Math.min(500, Math.max(350, Math.round(bodyWidth.value * .45))))
const actualWidth = computed(() => clamp(width.value ?? defaultWidth.value))

let observer: ResizeObserver | undefined
let drag: { pointer: number; x: number; startWidth: number; previous?: number } | undefined

function clamp(value: number) {
  return Math.min(maximum.value, Math.max(minimum, Math.round(value)))
}

function begin(event: PointerEvent) {
  if (!event.isPrimary || event.button !== 0 || drag || !divider.value) return
  const body = divider.value.parentElement
  if (!body) return
  bodyWidth.value = body.clientWidth
  const bounds = divider.value.getBoundingClientRect()
  drag = {
    pointer: event.pointerId,
    x: event.clientX,
    startWidth: body.getBoundingClientRect().right - bounds.left - bounds.width / 2,
    previous: width.value,
  }
  event.preventDefault()
  divider.value.focus({ preventScroll: true })
  divider.value.setPointerCapture(event.pointerId)
  window.addEventListener('blur', cancelDrag)
  emit('resizing', true)
}

function move(event: PointerEvent) {
  if (!drag || drag.pointer !== event.pointerId) return
  width.value = event.clientX === drag.x ? drag.previous : clamp(drag.startWidth + drag.x - event.clientX)
}

function finish(event: PointerEvent) {
  if (!drag || drag.pointer !== event.pointerId) return
  if (event.type !== 'pointerup') {
    cancelDrag()
    return
  }
  move(event)
  emit('save')
  releaseDrag()
}

function cancelDrag() {
  if (!drag) return
  const pointer = drag.pointer
  width.value = drag.previous
  releaseDrag()
  if (divider.value?.hasPointerCapture(pointer)) divider.value.releasePointerCapture(pointer)
}

function releaseDrag() {
  drag = undefined
  window.removeEventListener('blur', cancelDrag)
  emit('resizing', false)
}

function reset() {
  if (drag) return
  width.value = undefined
  emit('save')
}

function keydown(event: KeyboardEvent) {
  if (drag || event.altKey || event.ctrlKey || event.metaKey) return
  const step = event.shiftKey ? 48 : 16
  switch (event.key) {
    case 'ArrowLeft': width.value = clamp(actualWidth.value + step); break
    case 'ArrowRight': width.value = clamp(actualWidth.value - step); break
    case 'Home': width.value = maximum.value; break
    case 'End': width.value = minimum; break
    case 'Enter': event.preventDefault(); reset(); return
    default: return
  }
  event.preventDefault()
  emit('save')
}

onMounted(() => {
  const body = divider.value?.parentElement
  if (!body) return
  bodyWidth.value = body.clientWidth
  observer = new ResizeObserver(() => { bodyWidth.value = body.clientWidth })
  observer.observe(body)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  cancelDrag()
})
</script>

<template>
  <div ref="divider" class="review-divider" role="separator" tabindex="0"
    aria-label="调整改动审查宽度" aria-orientation="vertical" aria-controls="changes-drawer"
    :aria-valuemin="minimum" :aria-valuemax="maximum" :aria-valuenow="actualWidth" :aria-valuetext="actualWidth + ' 像素'"
    title="左右拖动调整宽度 · 双击恢复默认"
    @pointerdown="begin" @pointermove="move" @pointerup="finish" @pointercancel="finish" @lostpointercapture="finish"
    @dblclick="reset" @keydown="keydown">
    <span aria-hidden="true"></span>
  </div>
</template>
