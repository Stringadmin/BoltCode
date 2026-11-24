<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronDown, CircleAlert, CircleCheck, CircleX, ListTree } from '@lucide/vue'
import CopyButton from './CopyButton.vue'
import MarkdownMessage from './MarkdownMessage.vue'
import type { Message } from './native'

const props = defineProps<{ steps: Message[]; running: boolean; durationMs?: number }>()
const rendered = ref(props.running)
watch(() => props.running, (running) => { if (running) rendered.value = true })

const failed = computed(() => props.steps.some(step => step.role === 'tool' && step.failed))
const elapsed = computed(() => {
  if (props.durationMs === undefined) return ''
  if (props.durationMs < 1000) return '不到 1 秒'
  const seconds = Math.floor(props.durationMs / 1000)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor(seconds / 60) % 60
  return [hours && hours + ' 小时', minutes && minutes + ' 分钟', seconds % 60 + ' 秒'].filter(Boolean).join(' ')
})

const activityNames: Record<string, string> = {
  read_file: '读取文件', list_files: '浏览目录', search_code: '搜索代码', glob: '查找文件',
  write_file: '写入文件', edit_file: '修改文件', multi_edit: '批量修改',
  delete_file: '删除文件', rename_file: '重命名文件', run_command: '执行命令',
  get_diagnostics: '运行诊断', git_diff: '查看差异', git_status: '查看 Git 状态',
  git_commit: '提交改动', todo_write: '更新计划', update_memory: '更新项目记忆',
  web_fetch: '读取网页',
}

function showSteps(event: Event) {
  if ((event.currentTarget as HTMLDetailsElement).open) rendered.value = true
}
</script>

<template>
  <details class="turn-process" :open="running" @toggle="showSteps">
    <summary>
      <ListTree aria-hidden="true" />
      <span>执行过程</span><span v-if="elapsed" class="process-elapsed">用时 {{ elapsed }}</span>
      <CircleAlert v-if="failed" class="failure" aria-label="有步骤失败" />
      <ChevronDown class="activity-chevron" aria-hidden="true" />
    </summary>
    <ol v-if="rendered" class="process-steps">
      <li v-for="(step, index) in steps" :key="index">
        <div v-if="step.role === 'assistant'" class="process-note"><MarkdownMessage :text="step.text" /></div>
        <div v-else class="process-tool">
          <CircleX v-if="step.failed" class="failure" aria-label="失败" />
          <CircleCheck v-else class="success" aria-label="成功" />
          <span>{{ activityNames[step.text] || step.text }}</span>
          <code v-if="step.detail">{{ step.detail }}</code>
          <details v-if="step.output" class="tool-output">
            <summary>输出<ChevronDown /></summary>
            <div class="tool-output-body">
              <CopyButton :text="step.output" label="复制命令输出" />
              <pre>{{ step.output }}</pre>
            </div>
          </details>
        </div>
      </li>
    </ol>
  </details>
</template>
