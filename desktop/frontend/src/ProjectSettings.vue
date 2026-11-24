<script setup lang="ts">
import { ref, watch } from 'vue'
import { Archive, ArchiveRestore, FileText, FolderOpen, Shield, ShieldAlert } from '@lucide/vue'
import { native, type Repository, type State } from './native'

const props = defineProps<{ state: State; rules: string[] }>()
const emit = defineEmits<{ saved: []; access: []; archive: [path: string] }>()
const test = ref('')
const typecheck = ref('')
const busy = ref(false)
const error = ref('')
const saved = ref(false)

watch(() => props.state.projectChecks, checks => {
  test.value = checks.test
  typecheck.value = checks.typecheck
}, { immediate: true })

watch([test, typecheck], () => { saved.value = false })

async function save() {
  if (busy.value || !props.state.repository) return
  busy.value = true
  error.value = ''
  try {
    await native('SaveProjectChecks', props.state.repository, { test: test.value, typecheck: typecheck.value })
    saved.value = true
    emit('saved')
  } catch (cause) {
    error.value = String(cause instanceof Error ? cause.message : cause)
  } finally { busy.value = false }
}

async function restore(project: Repository) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await native('RestoreProject', project.path)
    emit('saved')
  } catch (cause) {
    error.value = String(cause instanceof Error ? cause.message : cause)
  } finally { busy.value = false }
}
</script>

<template>
  <section class="settings-page project-page">
    <div class="settings-page-heading"><h2>项目</h2></div>
    <div v-if="state.repository" class="settings-section">
      <div class="settings-subheading"><h3>当前项目</h3>
        <button class="action outline" type="button" :disabled="busy" @click="emit('archive', state.repository)"><Archive /> 归档项目</button>
      </div>
      <div class="settings-path"><FolderOpen /> <span><strong>{{ state.name }}</strong><small>{{ state.repository }}</small></span></div>
      <div v-if="rules.length" class="rule-sources">
        <span class="muted">最近一轮项目规则</span>
        <span v-for="path in rules" :key="path" class="rule-source"><FileText /> <code>{{ path }}</code></span>
      </div>
      <div class="settings-inline">
        <div><component :is="state.fullAccess ? ShieldAlert : Shield" /> <strong>访问权限</strong></div>
        <button class="action outline" type="button" :disabled="busy" @click="emit('access')">{{ state.fullAccess ? '完全访问' : '限制访问' }}</button>
      </div>
    </div>
    <form v-if="state.repository" class="settings-section settings-editor" @submit.prevent="save">
      <h3>检查命令</h3>
      <fieldset :disabled="busy" class="settings-fields">
        <label>测试命令<input v-model.trim="test" maxlength="1024" placeholder="例如 go test ./..." spellcheck="false" autocomplete="off"></label>
        <label>类型检查命令<input v-model.trim="typecheck" maxlength="1024" placeholder="例如 npm run typecheck" spellcheck="false" autocomplete="off"></label>
      </fieldset>
      <div class="actions"><button class="action primary" type="submit" :disabled="busy || saved">{{ saved ? '已保存' : '保存检查命令' }}</button></div>
    </form>
    <section class="settings-section">
      <h3>已归档项目</h3>
      <ul v-if="state.archivedProjects.length" class="settings-list">
        <li v-for="project in state.archivedProjects" :key="project.path">
          <Archive /><div><strong>{{ project.name }}</strong><small>{{ project.path }}</small></div>
          <button class="action outline" type="button" :disabled="busy" @click="restore(project)"><ArchiveRestore /> 恢复</button>
        </li>
      </ul>
      <p v-else class="muted">暂无归档项目。</p>
    </section>
    <p v-if="error" class="failure" role="alert">{{ error }}</p>
  </section>
</template>
