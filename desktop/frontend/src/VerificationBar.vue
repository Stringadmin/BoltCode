<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronRight, CircleCheck, FlaskConical, LoaderCircle, X } from '@lucide/vue'
import CopyButton from './CopyButton.vue'
import type { Verification } from './native'

const props = defineProps<{ status: Verification; disabled: boolean; error: string }>()
const emit = defineEmits<{ run: []; stop: []; configure: []; refresh: [] }>()
const dialog = ref<HTMLDialogElement>()
const labels: Record<string, string> = {
  pending: '未运行', running: '运行中', passed: '通过', failed: '失败', stale: '需重跑',
  cancelled: '已取消', timeout: '超时', error: '运行出错',
}
const checkName = (id: string) => id === 'typecheck' ? '类型检查' : '测试'
const tone = (status: string) => status === 'passed' ? 'success' : ['failed', 'timeout', 'error'].includes(status) ? 'failure' : 'muted'

const counts = computed(() => {
  const phases = ['passed', 'failed', 'error', 'timeout', 'stale', 'cancelled', 'pending']
  return phases.flatMap(status => {
    const count = props.status.checks.filter(check => check.status === status).length
    return count ? [labels[status] + ' ' + count] : []
  }).join(' · ')
})
const summary = computed(() => {
  if (props.status.running) {
    const current = props.status.checks.find(check => check.status === 'running')
    return current ? '正在运行' + checkName(current.id) : '正在准备检查'
  }
  if (props.status.changed) return props.status.checks.some(check => check.result) ? '工作区已变化 · 需重跑' : '工作区已变化'
  return counts.value || '尚未配置'
})
</script>

<template>
  <section class="verification-bar" aria-label="工作区验证">
    <div class="verification-row">
      <button class="verification-detail" type="button" :disabled="!status.checks.length" aria-label="查看验证详情"
        title="查看验证详情" @click="dialog?.showModal()">
        <LoaderCircle v-if="status.running" class="spinning" /><FlaskConical v-else />
        <strong>验证</strong><span :title="summary">{{ summary }}</span><ChevronRight v-if="status.checks.length" class="verification-chevron" />
      </button>
      <button v-if="status.running" type="button" class="action outline" @click="emit('stop')">停止</button>
      <button v-else-if="status.changed" type="button" class="action outline" :disabled="disabled" @click="emit('refresh')">刷新 Diff</button>
      <button v-else-if="status.checks.length" type="button" class="action outline" :disabled="disabled" @click="emit('run')">运行检查</button>
      <button v-else type="button" class="action outline" :disabled="disabled" @click="emit('configure')">配置检查</button>
    </div>
    <p v-if="error" class="review-error failure" role="alert">{{ error }}</p>
    <dialog ref="dialog" class="settings-dialog verification-dialog" aria-labelledby="verification-title"
      @keydown.ctrl.n.stop @keydown.ctrl.o.stop>
      <header>
        <h2 id="verification-title"><FlaskConical /> 验证详情</h2>
        <button class="quiet-icon" type="button" aria-label="关闭验证详情" @click="dialog?.close()"><X /></button>
      </header>
      <article v-for="check in status.checks" :key="check.id" class="check-detail">
        <header><strong>{{ checkName(check.id) }}</strong><span :class="tone(check.status)">{{ labels[check.status] }}</span></header>
        <code class="check-command">{{ check.command }}</code>
        <template v-if="check.result">
          <p v-if="check.command !== check.result.command" class="muted check-previous">上次命令 <code>{{ check.result.command }}</code></p>
          <p class="check-meta muted">
            {{ check.status === 'stale' || check.status === 'running' ? '上次运行 · ' : '' }}
            <CircleCheck v-if="check.status === 'passed'" />
            <span v-if="check.result.exitCode !== undefined">退出码 {{ check.result.exitCode }}</span>
            <span>{{ check.result.durationMs < 1000 ? check.result.durationMs + ' ms' : (check.result.durationMs / 1000).toFixed(1) + ' s' }}</span>
            <span>{{ new Date(check.result.finishedAt).toLocaleString('zh-CN') }}</span>
          </p>
          <div class="check-log">
            <CopyButton v-if="check.result.output" :text="check.result.output" label="复制检查日志" />
            <pre>{{ check.result.output || '命令没有输出。' }}</pre>
          </div>
        </template>
      </article>
      <div class="actions">
        <button class="action outline" type="button" :disabled="status.running || disabled" @click="dialog?.close(); emit('configure')">编辑检查</button>
        <button class="action outline" type="button" @click="dialog?.close()">关闭</button>
      </div>
    </dialog>
  </section>
</template>
