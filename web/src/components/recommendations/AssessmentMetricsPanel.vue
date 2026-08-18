<template>
  <section class="panel assessment-ops">
    <div class="section-title">
      <div>
        <h2>评估队列与 LLM 指标</h2>
        <span>文章爬取自动执行；LLM 评估排队等待，点击后单次消费当前待评估任务，不含 prompt 或正文</span>
      </div>
      <a-space wrap>
        <span class="queue-state" :class="`queue-state-${queue.state}`">{{ queueStateLabel }}</span>
        <a-button :loading="loading" @click="refreshPanel">
          刷新指标
        </a-button>
        <a-button type="primary" :loading="runningQueue" :disabled="!queue.canRun || queue.state === 'running'" @click="runQueue">
          执行 LLM 评估
        </a-button>
      </a-space>
    </div>
    <a-spin :loading="loading" class="metrics-spin">
      <p v-if="errorMessage" class="ops-error">{{ errorMessage }}</p>
      <div v-else class="metrics-grid">
        <div><strong>{{ metrics.pendingQueue }}</strong><span>待评估队列</span></div>
        <div><strong>{{ metrics.last24h.success }}</strong><span>近 24 小时成功</span></div>
        <div><strong>{{ metrics.last24h.failure }}</strong><span>近 24 小时失败</span></div>
        <div><strong>{{ metrics.articlesPerHour.toFixed(2) }}</strong><span>articles/hour</span></div>
        <div><strong>{{ metrics.tokenTotals.total }}</strong><span>token 合计</span></div>
        <div><strong>{{ metrics.tokenTotals.prompt }} / {{ metrics.tokenTotals.completion }}</strong><span>prompt / completion</span></div>
        <div><strong>{{ metrics.duration.p50Ms }} / {{ metrics.duration.p95Ms }}</strong><span>耗时 p50 / p95 ms</span></div>
        <div><strong>{{ (metrics.schemaRetryRate * 100).toFixed(1) }}%</strong><span>schema 重试率</span></div>
      </div>
      <div class="queue-summary">
        <div><strong>{{ queue.counts.pending }}</strong><span>等待执行</span></div>
        <div><strong>{{ queue.counts.running }}</strong><span>正在运行</span></div>
        <div><strong>{{ queue.counts.succeeded24h }}</strong><span>近 24 小时成功</span></div>
        <div><strong>{{ queue.counts.failed24h }}</strong><span>近 24 小时失败</span></div>
      </div>
    </a-spin>

    <div class="section-title backfill-title">
      <div>
        <h2>评估回填 / 回滚</h2>
        <span>回填写入手动评估队列，最多 250 篇；入队后仍需点击「执行 LLM 评估」才会调用模型</span>
      </div>
    </div>
    <form class="backfill-form" @submit.prevent="runBackfill(true)">
      <label class="source-field">
        <span>批次上限</span>
        <input v-model.number="limit" type="number" min="1" max="250" />
      </label>
      <label class="toggle-row">
        <input v-model="retryFailures" type="checkbox" />
        <span>重试失败项</span>
      </label>
      <a-space wrap>
        <a-button html-type="submit" :loading="running">预览回填</a-button>
        <a-button type="primary" :loading="running" @click="runBackfill(false)">执行回填</a-button>
        <a-button :loading="running" @click="runRollback(true)">预览回滚</a-button>
        <a-button status="danger" :loading="running" @click="runRollback(false)">执行回滚</a-button>
      </a-space>
    </form>
    <p v-if="batchMessage" class="batch-message">{{ batchMessage }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { Message } from '@arco-design/web-vue'

const props = defineProps<{ active: boolean }>()

interface AssessmentMetrics {
  pendingQueue: number
  last24h: { success: number; failure: number }
  articlesPerHour: number
  tokenTotals: { prompt: number; completion: number; reasoning: number; cached: number; total: number }
  duration: { p50Ms: number; p95Ms: number }
  schemaRetryRate: number
}

interface AssessmentQueueSnapshot {
  mode: 'automatic' | 'manual'
  state: 'idle' | 'waiting' | 'running'
  canRun: boolean
  counts: { pending: number; running: number; succeeded24h: number; failed24h: number }
}

const emptyQueue = (): AssessmentQueueSnapshot => ({
  mode: 'manual',
  state: 'idle',
  canRun: false,
  counts: { pending: 0, running: 0, succeeded24h: 0, failed24h: 0 },
})

const emptyMetrics = (): AssessmentMetrics => ({
  pendingQueue: 0,
  last24h: { success: 0, failure: 0 },
  articlesPerHour: 0,
  tokenTotals: { prompt: 0, completion: 0, reasoning: 0, cached: 0, total: 0 },
  duration: { p50Ms: 0, p95Ms: 0 },
  schemaRetryRate: 0,
})

const metrics = reactive(emptyMetrics())
const queue = reactive(emptyQueue())
const loading = ref(false)
const running = ref(false)
const runningQueue = ref(false)
const errorMessage = ref('')
const batchMessage = ref('')
const limit = ref(250)
const retryFailures = ref(false)
let queueTimer: ReturnType<typeof window.setTimeout> | null = null

const queueStateLabel = computed(() => ({
  idle: '队列空闲',
  waiting: '等待手动执行',
  running: '正在执行',
}[queue.state]))

function authHeaders(json = false): HeadersInit {
  const token = localStorage.getItem('token')
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  }
}

async function requestJSON<T>(url: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(url, options)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.Status === '0') {
    throw new Error(payload.Message || `HTTP ${response.status}`)
  }
  return payload.Data as T
}

// 加载 owner 评估面板的安全 token/耗时聚合。
const loadMetrics = async () => {
  try {
    loading.value = true
    errorMessage.value = ''
    const data = await requestJSON<AssessmentMetrics>('/api/admin/assessment/metrics', { headers: authHeaders() })
    Object.assign(metrics, emptyMetrics(), data, {
      last24h: { ...emptyMetrics().last24h, ...(data?.last24h || {}) },
      tokenTotals: { ...emptyMetrics().tokenTotals, ...(data?.tokenTotals || {}) },
      duration: { ...emptyMetrics().duration, ...(data?.duration || {}) },
    })
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载评估指标失败'
  } finally {
    loading.value = false
  }
}

const clearQueueTimer = () => {
  if (queueTimer !== null) {
    window.clearTimeout(queueTimer)
    queueTimer = null
  }
}

const scheduleQueueRefresh = () => {
  clearQueueTimer()
  if (!props.active) return
  const delay = queue.state === 'running' ? 2000 : 10000
  queueTimer = window.setTimeout(() => { void loadQueue(true) }, delay)
}

// 加载暂停中的 LLM 评估作业快照。
const loadQueue = async (silent = false) => {
  try {
    const data = await requestJSON<AssessmentQueueSnapshot>('/api/admin/assessment/queue?limit=50', { headers: authHeaders() })
    Object.assign(queue, emptyQueue(), data, {
      counts: { ...emptyQueue().counts, ...(data?.counts || {}) },
    })
  } catch (error) {
    if (!silent) {
      errorMessage.value = error instanceof Error ? error.message : '加载评估队列失败'
    }
  } finally {
    scheduleQueueRefresh()
  }
}

const refreshPanel = async () => {
  await Promise.all([loadMetrics(), loadQueue()])
}

const runQueue = async () => {
  try {
    runningQueue.value = true
    const data = await requestJSON<AssessmentQueueSnapshot>('/api/admin/assessment/queue/run', { method: 'POST', headers: authHeaders() })
    Object.assign(queue, emptyQueue(), data, {
      counts: { ...emptyQueue().counts, ...(data?.counts || {}) },
    })
    Message.success('评估任务队列已开始执行')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '启动评估任务队列失败')
  } finally {
    runningQueue.value = false
    scheduleQueueRefresh()
  }
}

const describeBatch = (action: string, result: { selected?: number; enqueued?: number; reactivated?: number; skipped?: number; dryRun?: boolean }) => {
  return `${action}${result.dryRun ? '预览' : ''}：选中 ${result.selected || 0}，入队 ${result.enqueued || 0}，激活 ${result.reactivated || 0}，跳过 ${result.skipped || 0}`
}

const runBackfill = async (dryRun: boolean) => {
  try {
    running.value = true
    const result = await requestJSON<any>('/api/admin/discovery/article-assessments/backfill', {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ limit: limit.value, dryRun, retryFailures: retryFailures.value }),
    })
    batchMessage.value = describeBatch('回填', result || {})
    Message.success(batchMessage.value)
    if (!dryRun) await refreshPanel()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '评估回填失败')
  } finally {
    running.value = false
  }
}

const runRollback = async (dryRun: boolean) => {
  try {
    running.value = true
    const result = await requestJSON<any>('/api/admin/discovery/article-assessments/rollback', {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ limit: limit.value, dryRun }),
    })
    batchMessage.value = describeBatch('回滚', result || {})
    Message.success(batchMessage.value)
    if (!dryRun) await refreshPanel()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '评估回滚失败')
  } finally {
    running.value = false
  }
}

watch(() => props.active, (active) => {
  if (active) {
    void refreshPanel()
    return
  }
  clearQueueTimer()
}, { immediate: true })

onUnmounted(() => {
  clearQueueTimer()
})
</script>

<style scoped>
/* 评估运营面板自带卡片样式：父页 scoped 的 .panel / .section-title 不会穿透到本组件 */
.assessment-ops {
  width: 100%;
  min-width: 0;
  padding: 4px 0 8px;
}

.section-title {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.section-title h2 {
  margin: 0 0 4px;
  font-size: 20px;
}

.section-title > div span {
  color: #86909c;
  font-size: 13px;
}

/* Arco Spin 默认 inline-block，会把网格收缩成单列 */
.metrics-spin,
.assessment-ops :deep(.arco-spin) {
  display: block;
  width: 100%;
}

.ops-error {
  color: #d93026;
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  width: 100%;
}

.metrics-grid > div {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  min-height: 96px;
  padding: 16px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #f7f8fa;
}

.metrics-grid strong {
  font-size: 22px;
  line-height: 1.25;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

.metrics-grid span {
  color: #86909c;
  font-size: 12px;
  line-height: 1.4;
}

.backfill-title {
  margin-top: 28px;
  margin-bottom: 12px;
}

.backfill-form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 16px 24px;
  width: 100%;
}

.source-field {
  display: grid;
  gap: 6px;
  color: #4e5969;
  font-size: 13px;
}

.source-field input {
  width: 140px;
  height: 32px;
  box-sizing: border-box;
  padding: 4px 10px;
  border: 1px solid #d9d9d9;
  border-radius: 6px;
}

.toggle-row {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  color: #4e5969;
  font-size: 13px;
}

.batch-message {
  margin: 12px 0 0;
  color: #4e5969;
}

.queue-state {
  padding: 5px 10px;
  border-radius: 999px;
  background: #f2f3f5;
  color: #4e5969;
  font-size: 13px;
}

.queue-state-running {
  background: #e8f3ff;
  color: #165dff;
}

.queue-state-waiting {
  background: #fff7e8;
  color: #d46b08;
}

.queue-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  width: 100%;
  margin-top: 16px;
}

.queue-summary > div {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  padding: 16px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #f7f8fa;
}

.queue-summary strong {
  font-size: 22px;
  font-variant-numeric: tabular-nums;
}

.queue-summary span {
  color: #86909c;
  font-size: 12px;
}

@media (max-width: 820px) {
  .metrics-grid,
  .queue-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .section-title,
  .backfill-form {
    grid-template-columns: 1fr;
    flex-direction: column;
  }
}
</style>
