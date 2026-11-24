<script setup lang="ts">
import { CircleCheck, FileJson } from '@lucide/vue'
import type { WorkspaceMemory } from './native'

defineProps<{ memory: WorkspaceMemory }>()

const factLabels: Record<string, string> = {
  package_manager: '包管理器', build_command: '构建命令', test_command: '测试命令',
}
const checkLabels: Record<string, string> = {
  test: '项目测试检查通过', typecheck: '项目类型检查通过',
}

function describeLesson(lesson: NonNullable<WorkspaceMemory['toolLessons']>[number]) {
  switch (lesson.kind) {
    case 'json_object': return '参数使用一个有效的 JSON 对象'
    case 'argument_type': return (lesson.field || '') + ' 参数使用 ' + (lesson.type || '') + ' 类型'
    case 'required_field': return '传入必填参数 ' + (lesson.field || '')
    case 'windows_shell': return 'Windows 默认使用 cmd.exe。PowerShell 命令需显式调用 PowerShell'
    default: return ''
  }
}
</script>

<template>
  <ul class="memory-evidence">
    <li v-for="fact in memory.facts" :key="fact.key">
      <FileJson aria-hidden="true" />
      <div>
        <span>{{ factLabels[fact.key] }}</span> <code>{{ fact.value }}</code>
        <small :title="'版本 ' + fact.version">{{ fact.source }}</small>
      </div>
    </li>
    <li v-for="check in memory.checks" :key="check.id">
      <CircleCheck class="success" aria-hidden="true" />
      <div>
        <span>{{ checkLabels[check.id] }}</span>
        <small :title="new Date(check.finishedAt).toLocaleString()">Git 快照 · {{ check.tree.slice(0, 12) }}</small>
      </div>
    </li>
    <li v-for="lesson in memory.toolLessons" :key="[lesson.tool, lesson.kind, lesson.field, lesson.type].join(':')">
      <CircleCheck class="success" aria-hidden="true" />
      <div><span>已验证的工具纠错</span> <code>{{ lesson.tool }}</code><small>{{ describeLesson(lesson) }}</small></div>
    </li>
  </ul>
</template>
