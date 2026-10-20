<template>
  <section class="batch-panel" aria-label="百炼 Batch 批次记录">
    <div class="batch-heading">
      <h3>{{ executionMode === 'batch' ? '百炼 Batch · 手动启动' : run?.active ? '旧 Batch · 继续回收' : '百炼 Batch · 历史记录' }}</h3>
      <a-button size="small" :loading="loading" @click="loadBatches(false)">刷新批次</a-button>
    </div>
    <p v-if="run">运行 #{{ run.id }} · {{ run.model }} · {{ phaseLabel(run.status) }} · {{ elapsedLabel }}</p>
    <p v-if="run" class="batch-counts">
      本轮 {{ run.counts.total }} 篇 · 待提交 {{ run.counts.pending }} · 处理中 {{ run.counts.processing }} ·
      成功 {{ run.counts.succeeded }} · 失败 {{ run.counts.failed }} · 复用 {{ run.counts.reused }} · 跳过 {{ run.counts.skipped }}
    </p>
    <p v-if="run?.status === 'paused'" role="alert" class="batch-error">
      本轮已暂停<span v-if="run.errorCode">：HTTP {{ run.errorCode }}</span>。
      {{ pauseHint(run.errorCode) }} {{ run.error }}
      已停止取队列及供应商请求，重启不会自动恢复。处理后点击「继续本轮评估」。
    </p>
    <p v-if="executionMode === 'batch' && run?.active && run.status !== 'paused' && !run.admissionEnabled && !cleanup" class="batch-note" role="status">
      重启后等待手动继续。已开始上传的批次仍会继续提交并回收结果；其余文章及尚未开始上传的批次，点击「继续本轮评估」后执行。
    </p>
    <div v-if="cleanup" role="alert" class="batch-error">
      <p>Batch 文件清理已暂停<span v-if="cleanup.errorCode">：HTTP {{ cleanup.errorCode }}</span>。{{ pauseHint(cleanup.errorCode) }} {{ cleanup.error }}</p>
      <p>处理后可重试文件清理<span v-if="run?.active">，恢复已有批次处理；剩余任务仍需按页面提示手动继续</span>。</p>
      <a-button size="small" :loading="resumingCleanup" @click="resumeCleanup">重试文件清理</a-button>
      <p v-if="cleanupError">{{ cleanupError }}</p>
    </div>
    <p v-if="run?.counts.returned" class="batch-note">已退回 River：{{ run.counts.returned }} 篇，等待手动执行评估。</p>
    <p class="batch-note">提交成功后仍需等待供应商结果。请求失败或输出校验失败请勾选「重试失败项」，执行回填后再次执行评估。</p>
    <p v-if="error" role="alert" class="batch-error">{{ error }}</p>
    <div class="batch-table-scroll">
      <table v-if="batches.length" class="batch-table">
        <thead><tr><th>批次 / 模型</th><th>状态</th><th>总数 / 供应商成功 / 失败</th><th>提交 / 截止时间</th><th>失败信息</th></tr></thead>
        <tbody>
          <tr v-for="batch in batches" :key="batch.id">
            <td>#{{ batch.id }} · {{ batch.model }}<small>{{ batch.remoteId || '尚未取得远端 ID' }}</small></td>
            <td>{{ phaseLabel(batch.phase) }}<small v-if="batch.providerStatus">{{ phaseLabel(batch.providerStatus) }}</small></td>
            <td>{{ batch.total }} / {{ batch.completed }} / {{ batch.failed }}</td>
            <td>{{ formatTime(batch.submittedAt) }}<small>截止 {{ formatTime(batch.expiresAt) }}</small></td>
            <td><span class="batch-error">{{ batch.error }}</span><a-button size="small" @click="openFailures(batch.id)">查看失败项</a-button></td>
          </tr>
        </tbody>
      </table>
      <p v-else>暂无已提交批次。</p>
    </div>
    <a-button v-if="hasMore" size="small" :loading="loading" @click="loadBatches(true)">更多批次</a-button>
    <a-modal
      v-model:visible="failureVisible"
      :title="`批次 #${selectedId} · 失败详情`"
      width="min(720px, calc(100vw - 32px))"
      :footer="false"
      unmount-on-close
      @close="closeFailures"
    >
      <div class="batch-failures">
        <p v-if="failureError" role="alert" class="batch-error">{{ failureError }}</p>
        <p v-if="failureLoading" role="status">正在加载失败项…</p>
        <p v-else-if="!failures.length && !failureError">暂无失败项。</p>
        <ul v-if="failures.length"><li v-for="item in failures" :key="item.id">文章 #{{ item.materialId }} · 版本 {{ item.contentVersion }}：{{ item.error }}</li></ul>
        <a-button v-if="failureError" size="small" :loading="failureLoading" @click="loadFailures(selectedId!, failures.length > 0)">重试</a-button>
        <a-button v-else-if="nextFailureId" size="small" :loading="failureLoading" @click="loadFailures(selectedId!, true)">更多失败项</a-button>
      </div>
    </a-modal>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { requestJSON } from '@/api/client'
import type { BatchRunSummary, BatchCleanupPause } from './batch-types'

const props = defineProps<{ active: boolean; run: BatchRunSummary | null; executionMode: string; cleanup?: BatchCleanupPause | null }>()
const emit = defineEmits<{ cleanupResumed: [] }>()
interface BatchRecord { id: number; model: string; phase: string; providerStatus: string; remoteId?: string; total: number; completed: number; failed: number; submittedAt?: string; expiresAt?: string; error?: string }
interface FailedItem { id: number; materialId: number; contentVersion: number; error: string }
const resumingCleanup = ref(false)
const cleanupError = ref('')
const pauseHint = (code?: number) => code === 401 ? '请检查 API 密钥和访问权限。' : code === 429 ? '请检查额度和请求限流。' : '请核对供应商任务和文件状态后再继续。'
const resumeCleanup = async () => {
  resumingCleanup.value = true
  cleanupError.value = ''
  try {
    await requestJSON('/api/admin/assessment/batches/cleanup/resume', { method: 'POST' })
    if (!disposed) emit('cleanupResumed')
  } catch (cause) { if (!disposed) cleanupError.value = cause instanceof Error ? cause.message : '恢复文件清理失败' }
  finally { if (!disposed) resumingCleanup.value = false }
}
const batches = ref<BatchRecord[]>([])
const loading = ref(false)
const error = ref('')
const hasMore = ref(false)
const selectedId = ref<number | null>(null)
const failureVisible = ref(false)
const failureLoading = ref(false)
const failureError = ref('')
const failures = ref<FailedItem[]>([])
const nextFailureId = ref(0)
let timer: ReturnType<typeof setTimeout> | null = null
let disposed = false
let version = 0
let failureVersion = 0
const labels: Record<string, string> = { paused: '已暂停，需要手动继续', rolled_back: '已退回 River', preparing: '准备本轮', processing: '处理本轮', prepared: '待上传', upload_unknown: '上传结果待核对', file_processing: '输入文件处理中', submission_unknown: '提交结果待核对', submitted: '已提交，等待结果', importing: '导入结果', done: '已结束', validating: '供应商验证中', in_progress: '供应商处理中', finalizing: '供应商生成结果', completed: '已完成', failed: '失败', expired: '已过期', cancelling: '取消中', cancelled: '已取消' }
const phaseLabel = (phase: string) => labels[phase] || phase
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '—'
const elapsedLabel = computed(() => {
  if (!props.run) return ''
  const end = props.run.finishedAt ? new Date(props.run.finishedAt).getTime() : Date.now()
  const minutes = Math.max(0, Math.floor((end - new Date(props.run.createdAt).getTime()) / 60000))
  return `经过 ${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分钟`
})
const schedule = () => {
  if (timer) clearTimeout(timer)
  if (props.active && !disposed) timer = setTimeout(() => void loadBatches(false), 10000)
}
const loadBatches = async (more: boolean) => {
  const current = ++version
  loading.value = true
  try {
    const before = more ? batches.value.at(-1)?.id || 0 : 0
    const rows = await requestJSON<BatchRecord[]>(`/api/admin/assessment/batches?limit=20&beforeId=${before}`)
    if (disposed || current !== version) return
    batches.value = more ? [...batches.value, ...rows] : rows
    hasMore.value = rows.length === 20
    error.value = ''
  } catch (cause) {
    if (!disposed && current === version) error.value = cause instanceof Error ? cause.message : '查询批次失败'
  } finally {
    if (!disposed && current === version) { loading.value = false; schedule() }
  }
}
const openFailures = (id: number) => {
  selectedId.value = id
  failures.value = []
  nextFailureId.value = 0
  failureError.value = ''
  failureVisible.value = true
  void loadFailures(id, false)
}
const closeFailures = () => {
  failureVisible.value = false
  failureVersion++
  failureLoading.value = false
  selectedId.value = null
  failures.value = []
  nextFailureId.value = 0
  failureError.value = ''
}
const loadFailures = async (id: number, more: boolean) => {
  const current = ++failureVersion
  failureLoading.value = true
  failureError.value = ''
  try {
    const after = more ? nextFailureId.value : 0
    const data = await requestJSON<{ items: FailedItem[]; nextId: number }>(`/api/admin/assessment/batches/${id}?limit=50&afterId=${after}`)
    if (disposed || current !== failureVersion || !failureVisible.value || selectedId.value !== id) return
    failures.value = more ? [...failures.value, ...data.items] : data.items
    nextFailureId.value = data.nextId
  } catch (cause) {
    if (!disposed && current === failureVersion && failureVisible.value) failureError.value = cause instanceof Error ? cause.message : '查询失败项失败'
  } finally {
    if (!disposed && current === failureVersion) failureLoading.value = false
  }
}
watch(() => props.active, active => { if (active) void loadBatches(false); else { version++; loading.value = false; closeFailures(); if (timer) clearTimeout(timer) } }, { immediate: true })
onUnmounted(() => { disposed = true; version++; failureVersion++; if (timer) clearTimeout(timer) })
</script>

<style scoped>
.batch-panel { margin: 24px 0; padding: 16px; border: 1px solid var(--color-border-2); border-radius: 8px; }
.batch-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
h3, h4 { margin: 0 0 12px; }
.batch-note, small { color: var(--color-text-3); }
.batch-counts { line-height: 1.8; }
.batch-table-scroll { overflow-x: auto; }
.batch-table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { padding: 10px; text-align: left; border-bottom: 1px solid var(--color-border-2); vertical-align: top; }
small { display: block; margin-top: 5px; overflow-wrap: anywhere; }
.batch-error { color: #d93026; overflow-wrap: anywhere; }
.batch-failures { max-height: 60vh; overflow-y: auto; overflow-wrap: anywhere; }
.batch-failures ul { padding-left: 20px; }
.batch-failures li { margin-bottom: 10px; line-height: 1.6; }
</style>
