<template>
  <section class="workflow-panel">
    <div class="workflow-heading">
      <div>
        <h2>人工标注工作流</h2>
        <p>从 120 篇不可变正文样本中至少标注 30 篇，可继续多标或跳过，再完成盲重复、冲突复核和模型验收。</p>
      </div>
      <a-space wrap>
        <a-button :loading="loading" @click="loadSummary">刷新</a-button>
        <a-button v-if="!summary.exists" type="primary" :loading="creating" @click="createWorkflow">创建标注批次</a-button>
      </a-space>
    </div>

    <a-alert v-if="errorMessage" type="error" :show-icon="true" closable @close="errorMessage = ''">
      {{ errorMessage }}
    </a-alert>

    <template v-if="summary.exists">
      <div class="workflow-meta">
        <span>批次 #{{ summary.runId }}</span>
        <span>{{ summary.policyVersion }}</span>
        <span class="status-pill">{{ statusLabel }}</span>
        <span v-if="summary.model">模型 {{ summary.model }}</span>
      </div>

      <div class="workflow-steps">
        <article :class="stepClass(1)"><strong>1</strong><div><b>第一轮盲标</b><span>标注 {{ summary.passOne.labeled }}/{{ summary.passOne.required || 30 }}+ · 跳过 {{ summary.passOne.skipped }}</span></div></article>
        <article :class="stepClass(2)"><strong>2</strong><div><b>72 小时间隔</b><span>{{ waitingText }}</span></div></article>
        <article :class="stepClass(3)"><strong>3</strong><div><b>第二轮盲标</b><span>{{ summary.passTwo.labeled }}/{{ summary.passTwo.total || 30 }}</span></div></article>
        <article :class="stepClass(4)"><strong>4</strong><div><b>冲突复核</b><span>{{ summary.adjudication.labeled }}/{{ summary.adjudication.total }}</span></div></article>
        <article :class="stepClass(5)"><strong>5</strong><div><b>模型验收</b><span>{{ summary.evaluationProgress.labeled }}/{{ summary.evaluationProgress.total || 240 }}</span></div></article>
      </div>

      <a-alert v-if="summary.status === 'waiting_pass_two'" type="info" :show-icon="true">
        第二轮必须与第一轮完成时间间隔至少 72 小时。{{ waitingText }}
      </a-alert>

      <a-alert v-if="summary.status === 'pass_one'" type="info" :show-icon="true">
        样本池共 {{ summary.passOne.total }} 篇。至少保存 {{ summary.passOne.required || 30 }} 篇评分即可完成第一轮；其余文章可逐篇跳过，也可不再处理。完成第一轮前仍可继续多标。
      </a-alert>

      <template v-if="labelPass > 0">
        <details class="rubric" open>
          <summary>统一评分锚点</summary>
          <div class="rubric-grid">
            <span><b>0–19</b> 几乎没有有效价值</span><span><b>20–39</b> 较弱、信息或论证不足</span>
            <span><b>40–59</b> 普通，有一些有效信息</span><span><b>60–74</b> 良好、具体且完整</span>
            <span><b>75–89</b> 优秀、深入且可复用</span><span><b>90–100</b> 极少数卓越文章</span>
          </div>
          <p>Quality 是总体阅读收获；Depth 是机制、因果、权衡、限制、反例或实验的深度；Evergreen 是脱离即时新闻和版本后仍可复用的程度。不要考虑站点、作者声誉、热度和发布时间。</p>
        </details>

        <a-spin :loading="itemLoading" class="label-card">
          <div v-if="item" class="label-content">
            <div class="label-toolbar">
              <strong>第 {{ labelPass }} 轮 · {{ item.position + 1 }}/{{ item.total }}</strong>
              <span v-if="labelPass === 1">已标注 {{ currentProgress.labeled }} · 已跳过 {{ currentProgress.skipped }}</span>
              <span v-else>已保存 {{ currentProgress.labeled }}/{{ currentProgress.total }}</span>
            </div>
            <h3>{{ item.title || '（无标题）' }}</h3>
            <p class="article-meta">语言 {{ item.language || 'unknown' }} · 正文 {{ item.bodyCharacters }} 字符 · 样本 {{ item.sampleId }} <b v-if="item.skipped" class="skipped-pill">已跳过</b></p>
            <article class="article-body">{{ item.bodyText }}</article>

            <div class="score-grid">
              <label>Quality 0–100<input v-model.number="form.scores.quality" type="number" min="0" max="100" step="1" :disabled="form.unjudgeable" /></label>
              <label>Depth 0–100<input v-model.number="form.scores.depth" type="number" min="0" max="100" step="1" :disabled="form.unjudgeable" /></label>
              <label>Evergreen 0–100<input v-model.number="form.scores.evergreen" type="number" min="0" max="100" step="1" :disabled="form.unjudgeable" /></label>
            </div>
            <label class="field">简短人工理由<textarea v-model="form.reason" maxlength="300" :disabled="form.unjudgeable" /></label>
            <label class="field">文章类型
              <select v-model="form.genre" :disabled="form.unjudgeable">
                <option value="">请选择</option><option value="analysis">analysis</option><option value="tutorial">tutorial</option>
                <option value="reference">reference</option><option value="essay">essay</option><option value="news">news</option>
                <option value="release">release</option><option value="personal-update">personal-update</option><option value="other">other</option>
              </select>
            </label>
            <div class="label-checks">
              <label><input v-model="form.extractionBad" type="checkbox" /> 正文提取有明显问题</label>
              <label><input v-model="form.unjudgeable" type="checkbox" /> 无法判断</label>
            </div>
            <div class="label-actions">
              <a-button :loading="saving" :disabled="item.position === 0" @click="saveAndMove(-1)">保存并上一篇</a-button>
              <a-button v-if="labelPass === 1" status="warning" :loading="skipping" @click="skipCurrent">{{ item.position + 1 >= item.total ? '跳过当前' : '跳过并下一篇' }}</a-button>
              <a-button :loading="saving" @click="saveCurrent">保存当前</a-button>
              <a-button type="primary" :loading="saving" :disabled="item.position + 1 >= item.total" @click="saveAndMove(1)">保存并下一篇</a-button>
            </div>
          </div>
        </a-spin>
      </template>

      <div v-if="showAdvance" class="phase-action">
        <div><strong>{{ advanceTitle }}</strong><p>{{ advanceDescription }}</p></div>
        <a-button type="primary" :disabled="!canAdvanceNow" :loading="advancing" @click="advanceWorkflow">{{ advanceButton }}</a-button>
      </div>

      <div v-if="summary.status === 'human_complete' || summary.status === 'evaluation_failed' || (summary.status === 'complete' && summary.canEvaluate)" class="phase-action">
        <div>
          <strong>人工金标已经完成</strong>
          <p>后台将用生产模型对 120 篇文章各评分两次，共 240 个结果；token 只记录计数，不保存 prompt 或 completion 文本。</p>
          <small v-if="summary.evaluationError" class="error-text">上次失败：{{ summary.evaluationError }}</small>
        </div>
        <a-button type="primary" :loading="evaluating" @click="startEvaluation">{{ summary.status === 'human_complete' ? '运行模型双跑验收' : '重新运行模型验收' }}</a-button>
      </div>

      <div v-if="summary.status === 'evaluating'" class="evaluation-running">
        <a-spin :loading="true" />
        <div><strong>模型验收正在后台运行</strong><p>已写入 {{ summary.evaluationProgress.labeled }}/{{ summary.evaluationProgress.total }} 个评分结果，页面每 3 秒刷新。</p></div>
      </div>

      <div v-if="summary.report" class="report-card">
        <div class="report-result" :class="summary.report.activationReady ? 'ready' : 'blocked'">
          <strong>{{ summary.report.activationReady ? '达到 active 启用门槛' : '尚未达到 active 启用门槛' }}</strong>
          <span>未解决冲突 {{ summary.report.unresolvedConflicts }} · 排除样本 {{ summary.report.excludedSamples }}</span>
        </div>
        <div class="metric-grid">
          <span><b>{{ percent(summary.report.protocol.validOutputRate) }}</b>合法输出率</span>
          <span><b>{{ summary.report.protocol.reasoningTokens }}</b>Reasoning tokens</span>
          <span><b>{{ summary.report.protocol.promptTokenP95 }}</b>Prompt p95</span>
          <span><b>{{ fixed(summary.report.modelCore.quality.spearman) }}</b>Quality Spearman</span>
          <span><b>{{ fixed(summary.report.modelCore.quality.mae) }}</b>Quality MAE</span>
          <span><b>{{ percent(summary.report.humanConsistency.quality.bandAgreement) }}</b>人工分档一致率</span>
        </div>
        <div class="check-list">
          <span v-for="(passed, name) in summary.report.activationChecks" :key="name" :class="passed ? 'check-pass' : 'check-fail'">{{ passed ? '✓' : '×' }} {{ checkLabel(name) }}</span>
        </div>
      </div>
    </template>

    <div v-else-if="!loading" class="empty-workflow">
      <h3>尚未创建人工标注批次</h3>
      <p>创建后，样本和评分都只保存在服务端数据库；浏览器不会下载包含完整正文的离线文件。</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Message, Modal } from '@arco-design/web-vue'
import { hasArticleAssessmentInput } from '@/utils/articleAssessmentForm.mjs'

type WorkflowStatus = 'pass_one' | 'waiting_pass_two' | 'pass_two' | 'adjudication' | 'human_complete' | 'evaluating' | 'complete' | 'evaluation_failed'
interface Progress { total: number; labeled: number; skipped: number; required: number }
interface AxisScores { quality: number | null; depth: number | null; evergreen: number | null }
interface LabelInput { scores: AxisScores; reason: string; genre: string; extractionBad: boolean; unjudgeable: boolean; durationSeconds: number }
interface WorkflowItem { runId: number; pass: number; position: number; total: number; sampleId: string; title: string; bodyText: string; language: string; bodyCharacters: number; skipped: boolean; label?: LabelInput }
interface AxisMetric { spearman: number; mae: number; bandAgreement: number }
interface WorkflowReport {
  activationReady: boolean; unresolvedConflicts: number; excludedSamples: number
  activationChecks: Record<string, boolean>
  protocol: { validOutputRate: number; reasoningTokens: number; promptTokenP95: number }
  modelCore: { quality: AxisMetric }
  humanConsistency: { quality: AxisMetric }
}
interface WorkflowSummary {
  exists: boolean; runId: number; status: WorkflowStatus | ''; policyVersion: string; createdAt: string
  passOne: Progress; passTwo: Progress; adjudication: Progress
  nextPassAvailableAt?: string; canAdvance: boolean; canEvaluate: boolean
  evaluationGeneration: number; evaluationProgress: Progress; model?: string; promptVersion?: string; evaluationError?: string
  report?: WorkflowReport
}

const props = defineProps<{ active: boolean }>()
const emptySummary = (): WorkflowSummary => ({
  exists: false, runId: 0, status: '', policyVersion: '', createdAt: '',
  passOne: { total: 120, labeled: 0, skipped: 0, required: 30 }, passTwo: { total: 0, labeled: 0, skipped: 0, required: 0 }, adjudication: { total: 0, labeled: 0, skipped: 0, required: 0 },
  canAdvance: false, canEvaluate: false, evaluationGeneration: 0, evaluationProgress: { total: 0, labeled: 0, skipped: 0, required: 0 },
})
const summary = ref<WorkflowSummary>(emptySummary())
const item = ref<WorkflowItem | null>(null)
const currentPosition = ref(0)
const loading = ref(false)
const creating = ref(false)
const itemLoading = ref(false)
const saving = ref(false)
const skipping = ref(false)
const advancing = ref(false)
const evaluating = ref(false)
const errorMessage = ref('')
const now = ref(Date.now())
const form = reactive<LabelInput>({ scores: { quality: null, depth: null, evergreen: null }, reason: '', genre: '', extractionBad: false, unjudgeable: false, durationSeconds: 0 })
let openedAt = Date.now()
let pollTimer: ReturnType<typeof window.setTimeout> | null = null
let clockTimer: ReturnType<typeof window.setInterval> | null = null

const authHeaders = (json = false): Record<string, string> => {
  const token = localStorage.getItem('token') || sessionStorage.getItem('token')
  return { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(json ? { 'Content-Type': 'application/json' } : {}) }
}
const requestJSON = async <T>(url: string, options: RequestInit = {}): Promise<T> => {
  const response = await fetch(url, options)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.Status === '0') throw new Error(payload.Error || payload.Message || `HTTP ${response.status}`)
  return payload.Data as T
}

const labelPassByStatus: Partial<Record<WorkflowStatus, number>> = { pass_one: 1, pass_two: 2, adjudication: 3 }
const statusLabels: Record<WorkflowStatus, string> = {
  pass_one: '第一轮盲标', waiting_pass_two: '等待第二轮', pass_two: '第二轮盲标', adjudication: '冲突复核',
  human_complete: '人工金标完成', evaluating: '模型验收中', complete: '验收报告完成', evaluation_failed: '模型验收失败',
}
const labelPass = computed(() => summary.value.status ? labelPassByStatus[summary.value.status] || 0 : 0)
const currentProgress = computed(() => labelPass.value === 1 ? summary.value.passOne : labelPass.value === 2 ? summary.value.passTwo : summary.value.adjudication)
const statusLabel = computed(() => summary.value.status ? statusLabels[summary.value.status] : '')
const waitingText = computed(() => {
  if (!summary.value.nextPassAvailableAt) return '尚未开始计时'
  const remaining = new Date(summary.value.nextPassAvailableAt).getTime() - now.value
  if (remaining <= 0) return '现在可以开始第二轮'
  const hours = Math.floor(remaining / 3_600_000)
  const minutes = Math.ceil((remaining % 3_600_000) / 60_000)
  return `还需 ${hours} 小时 ${minutes} 分钟`
})
const showAdvance = computed(() => ['pass_one', 'waiting_pass_two', 'pass_two', 'adjudication'].includes(summary.value.status))
const canAdvanceNow = computed(() => summary.value.canAdvance || (summary.value.status === 'waiting_pass_two' && !!summary.value.nextPassAvailableAt && now.value >= new Date(summary.value.nextPassAvailableAt).getTime()))
const advanceTitles: Partial<Record<WorkflowStatus, string>> = {
  pass_one: '完成第一轮并开始 72 小时盲期', waiting_pass_two: '开始第二轮盲标', pass_two: '完成第二轮并识别冲突', adjudication: '完成冲突复核',
}
const advanceDescriptions: Partial<Record<WorkflowStatus, string>> = {
  pass_one: '保存至少 30 篇后即可提交，也可以继续多标。提交时未标注文章会记为跳过，第一轮随即冻结。', waiting_pass_two: '到达解锁时间后，从第一轮已标注文章中固定抽取 30 篇，优先保持 20 篇核心样本和 10 篇压力样本。',
  pass_two: '跨评分档或任一维度相差超过 15 分的文章会进入复核。', adjudication: '复核完成后，人工金标被冻结并可运行模型验收。',
}
const advanceButtons: Partial<Record<WorkflowStatus, string>> = { pass_one: '完成第一轮', waiting_pass_two: '开始第二轮', pass_two: '完成第二轮', adjudication: '完成人工金标' }
const advanceTitle = computed(() => summary.value.status ? advanceTitles[summary.value.status] || '进入下一阶段' : '进入下一阶段')
const advanceDescription = computed(() => summary.value.status ? advanceDescriptions[summary.value.status] || '' : '')
const advanceButton = computed(() => summary.value.status ? advanceButtons[summary.value.status] || '下一阶段' : '下一阶段')

function phaseIndex(status: WorkflowStatus | ''): number {
  if (!status) return 0
  const phases: Record<WorkflowStatus, number> = { pass_one: 1, waiting_pass_two: 2, pass_two: 3, adjudication: 4, human_complete: 5, evaluating: 5, complete: 6, evaluation_failed: 5 }
  return phases[status]
}
function stepClass(step: number) { const current = phaseIndex(summary.value.status); return { active: current === step, complete: current > step } }

async function loadSummary() {
  if (!props.active) return
  try {
    loading.value = true
    errorMessage.value = ''
    summary.value = await requestJSON<WorkflowSummary>('/api/admin/recommendations/article-assessment-workflow', { headers: authHeaders() })
    await syncCurrentItem()
    schedulePoll()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载人工标注工作流失败'
  } finally { loading.value = false }
}

async function createWorkflow() {
  Modal.confirm({
    title: '创建 120 篇人工标注批次？',
    content: '样本会固定引用当前不可变正文版本，并在数据库中持久化。',
    onOk: async () => {
      try {
        creating.value = true
        summary.value = await requestJSON<WorkflowSummary>('/api/admin/recommendations/article-assessment-workflow/runs', { method: 'POST', headers: authHeaders(true), body: '{}' })
        currentPosition.value = 0
        await loadItem(0)
        Message.success('人工标注批次已创建')
      } catch (error) { errorMessage.value = error instanceof Error ? error.message : '创建失败' }
      finally { creating.value = false }
    },
  })
}

async function syncCurrentItem() {
  if (!labelPass.value) { item.value = null; return }
  const progress = currentProgress.value
  const completed = labelPass.value === 1 ? progress.labeled + progress.skipped : progress.labeled
  const target = Math.max(0, Math.min(currentPosition.value || completed, Math.max(0, progress.total - 1)))
  currentPosition.value = target
  await loadItem(target)
}

async function loadItem(position: number) {
  if (!summary.value.runId || !labelPass.value) return
  try {
    itemLoading.value = true
    item.value = await requestJSON<WorkflowItem>(`/api/admin/recommendations/article-assessment-workflow/runs/${summary.value.runId}/items/${labelPass.value}/${position}`, { headers: authHeaders() })
    currentPosition.value = position
    hydrateForm(item.value.label)
  } catch (error) { errorMessage.value = error instanceof Error ? error.message : '加载文章失败' }
  finally { itemLoading.value = false }
}

function hydrateForm(label?: LabelInput) {
  form.scores.quality = label?.scores?.quality ?? null
  form.scores.depth = label?.scores?.depth ?? null
  form.scores.evergreen = label?.scores?.evergreen ?? null
  form.reason = label?.reason || ''
  form.genre = label?.genre || ''
  form.extractionBad = !!label?.extractionBad
  form.unjudgeable = !!label?.unjudgeable
  form.durationSeconds = label?.durationSeconds || 0
  openedAt = Date.now()
}

function validateForm(): string {
  if (form.unjudgeable) return ''
  const values = [form.scores.quality, form.scores.depth, form.scores.evergreen]
  if (values.some(value => !Number.isInteger(value) || Number(value) < 0 || Number(value) > 100)) return '请填写三个 0–100 的整数分数'
  if (!form.reason.trim()) return '请填写简短人工理由'
  if (!form.genre) return '请选择文章类型'
  return ''
}

async function saveCurrent(): Promise<boolean> {
  if (!item.value) return false
  const validation = validateForm()
  if (validation) { Message.warning(validation); return false }
  try {
    saving.value = true
    const durationSeconds = form.durationSeconds + Math.max(0, Math.round((Date.now() - openedAt) / 1000))
    summary.value = await requestJSON<WorkflowSummary>(`/api/admin/recommendations/article-assessment-workflow/runs/${summary.value.runId}/labels/${labelPass.value}/${item.value.sampleId}`, {
      method: 'PUT', headers: authHeaders(true), body: JSON.stringify({ ...form, durationSeconds }),
    })
    form.durationSeconds = durationSeconds
    item.value.skipped = false
    item.value.label = JSON.parse(JSON.stringify({ ...form, durationSeconds })) as LabelInput
    openedAt = Date.now()
    Message.success('评分已保存')
    return true
  } catch (error) { errorMessage.value = error instanceof Error ? error.message : '保存评分失败'; return false }
  finally { saving.value = false }
}

async function skipCurrent() {
  if (!item.value || labelPass.value !== 1) return
  if (item.value.label) {
    Modal.confirm({
      title: '跳过这篇已标注文章？',
      content: '跳过会删除这篇文章当前保存的第一轮评分，并相应减少已标注数量。',
      onOk: performSkip,
    })
    return
  }
  await performSkip()
}

async function performSkip() {
  if (!item.value || labelPass.value !== 1) return
  try {
    skipping.value = true
    summary.value = await requestJSON<WorkflowSummary>(`/api/admin/recommendations/article-assessment-workflow/runs/${summary.value.runId}/skips/1/${item.value.sampleId}`, {
      method: 'PUT', headers: authHeaders(true), body: '{}',
    })
    Message.success('已跳过当前文章')
    const target = item.value.position + 1
    if (target < item.value.total) await loadItem(target)
    else await loadItem(item.value.position)
  } catch (error) { errorMessage.value = error instanceof Error ? error.message : '跳过文章失败' }
  finally { skipping.value = false }
}

async function saveAndMove(delta: number) {
  if (!item.value) return
  if (delta < 0 && !hasArticleAssessmentInput(form)) {
    await loadItem(item.value.position + delta)
    return
  }
  if (!(await saveCurrent())) return
  const target = item.value.position + delta
  if (target >= 0 && target < item.value.total) await loadItem(target)
}

async function advanceWorkflow() {
  if (summary.value.status === 'pass_one') {
    Modal.confirm({
      title: `完成第一轮（已标注 ${summary.value.passOne.labeled} 篇）？`,
      content: `提交后第一轮会被冻结，未标注的 ${summary.value.passOne.total - summary.value.passOne.labeled} 篇将记为跳过。`,
      onOk: performAdvance,
    })
    return
  }
  await performAdvance()
}

async function performAdvance() {
  try {
    advancing.value = true
    summary.value = await requestJSON<WorkflowSummary>(`/api/admin/recommendations/article-assessment-workflow/runs/${summary.value.runId}/advance`, { method: 'POST', headers: authHeaders(true), body: '{}' })
    currentPosition.value = 0
    await syncCurrentItem()
    Message.success('已进入下一阶段')
  } catch (error) { errorMessage.value = error instanceof Error ? error.message : '无法进入下一阶段' }
  finally { advancing.value = false }
}

async function startEvaluation() {
  Modal.confirm({
    title: '运行 240 个模型评分？',
    content: '任务使用生产 LLM 配置、并发 2，在后台执行。失败结果也会保留在验收报告中。',
    onOk: async () => {
      try {
        evaluating.value = true
        summary.value = await requestJSON<WorkflowSummary>(`/api/admin/recommendations/article-assessment-workflow/runs/${summary.value.runId}/evaluate`, { method: 'POST', headers: authHeaders(true), body: '{}' })
        schedulePoll()
        Message.success('模型验收已启动')
      } catch (error) { errorMessage.value = error instanceof Error ? error.message : '启动模型验收失败' }
      finally { evaluating.value = false }
    },
  })
}

function schedulePoll() {
  if (pollTimer) window.clearTimeout(pollTimer)
  if (props.active && summary.value.status === 'evaluating') pollTimer = window.setTimeout(() => void loadSummary(), 3000)
}
function percent(value = 0) { return `${(value * 100).toFixed(1)}%` }
function fixed(value = 0) { return Number(value).toFixed(2) }
function checkLabel(name: string) { return name.replaceAll('_', ' ') }

watch(() => props.active, active => { if (active) void loadSummary(); else if (pollTimer) window.clearTimeout(pollTimer) }, { immediate: true })
clockTimer = window.setInterval(() => { now.value = Date.now() }, 30_000)
onBeforeUnmount(() => { if (pollTimer) window.clearTimeout(pollTimer); if (clockTimer) window.clearInterval(clockTimer) })
</script>

<style scoped>
.workflow-panel{width:100%;min-width:0;background:#fff;border:1px solid #e5e6eb;border-radius:12px;padding:22px}.workflow-heading{display:flex;justify-content:space-between;gap:20px;align-items:flex-start}.workflow-heading h2{margin:0 0 6px}.workflow-heading p,.phase-action p,.evaluation-running p{margin:0;color:#86909c}.workflow-meta{display:flex;gap:10px;flex-wrap:wrap;margin:18px 0}.workflow-meta span{padding:5px 10px;border-radius:999px;background:#f2f3f5;color:#4e5969}.workflow-meta .status-pill{background:#e8f3ff;color:#165dff}.workflow-steps{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px;margin-bottom:18px}.workflow-steps article{display:flex;gap:10px;padding:12px;border:1px solid #e5e6eb;border-radius:10px;color:#86909c}.workflow-steps article>strong{display:grid;place-items:center;width:28px;height:28px;border-radius:50%;background:#f2f3f5}.workflow-steps article div{display:flex;flex-direction:column}.workflow-steps article span{font-size:12px}.workflow-steps article.active{border-color:#165dff;color:#1d2129;background:#f2f7ff}.workflow-steps article.active>strong{background:#165dff;color:#fff}.workflow-steps article.complete>strong{background:#00b42a;color:#fff}.rubric{margin:18px 0;border:1px solid #e5e6eb;border-radius:10px;padding:14px}.rubric summary{font-weight:700;cursor:pointer}.rubric-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin:12px 0}.rubric-grid span{padding:8px;background:#f7f8fa;border-radius:6px}.rubric p{margin:8px 0 0;color:#4e5969}.label-card{display:block;border:1px solid #e5e6eb;border-radius:12px;padding:18px}.label-toolbar{display:flex;justify-content:space-between;color:#4e5969}.label-content h3{font-size:22px;margin:18px 0 4px}.article-meta{color:#86909c}.skipped-pill{display:inline-block;margin-left:6px;padding:2px 7px;border-radius:999px;background:#fff7e8;color:#d25f00}.article-body{white-space:pre-wrap;max-height:52vh;overflow:auto;padding:18px 4px;border-block:1px solid #e5e6eb;font:17px/1.75 ui-serif,Georgia,serif}.score-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:18px}.score-grid label,.field{display:flex;flex-direction:column;gap:6px;font-weight:600}.score-grid input,.field textarea,.field select{border:1px solid #c9cdd4;border-radius:6px;padding:9px;background:#fff}.field{margin-top:14px}.field textarea{min-height:74px;resize:vertical}.label-checks{display:flex;gap:24px;margin:16px 0}.label-actions{display:flex;justify-content:flex-end;gap:10px}.phase-action,.evaluation-running{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-top:18px;padding:18px;border-radius:10px;background:#f7f8fa}.evaluation-running{justify-content:flex-start}.error-text{display:block;color:#f53f3f;margin-top:8px}.report-card{margin-top:18px;border:1px solid #e5e6eb;border-radius:12px;padding:18px}.report-result{display:flex;justify-content:space-between;padding:14px;border-radius:8px}.report-result.ready{background:#e8ffea;color:#00b42a}.report-result.blocked{background:#fff7e8;color:#d25f00}.metric-grid{display:grid;grid-template-columns:repeat(6,1fr);gap:10px;margin:14px 0}.metric-grid span{display:flex;flex-direction:column;padding:10px;background:#f7f8fa;border-radius:8px;color:#86909c}.metric-grid b{font-size:20px;color:#1d2129}.check-list{display:flex;gap:8px;flex-wrap:wrap}.check-list span{padding:5px 8px;border-radius:6px;font-size:12px}.check-pass{background:#e8ffea;color:#00b42a}.check-fail{background:#ffece8;color:#f53f3f}.empty-workflow{text-align:center;padding:60px 20px;color:#86909c}@media(max-width:1000px){.workflow-steps{grid-template-columns:1fr 1fr}.rubric-grid,.metric-grid{grid-template-columns:repeat(2,1fr)}}@media(max-width:680px){.workflow-heading,.phase-action,.report-result{flex-direction:column}.workflow-steps,.rubric-grid,.score-grid,.metric-grid{grid-template-columns:1fr}.label-actions{flex-wrap:wrap}.article-body{max-height:46vh}}
</style>
