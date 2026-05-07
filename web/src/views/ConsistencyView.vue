<template>
  <div class="consistency-view">
    <div class="back-button-container">
      <a-button type="text" @click="goBack" class="back-button">
        <template #icon>
          <a-icon-arrow-left />
        </template>
        返回首页
      </a-button>
    </div>

    <main class="consistency-container">
      <div class="header-section">
        <div class="icon-wrapper">
          <a-icon-sync class="header-icon" />
        </div>
        <h1 class="page-title">归档一致性</h1>
        <p class="page-subtitle">HTML、Meilisearch 与数据库状态</p>
      </div>

      <section class="toolbar-band">
        <div class="toolbar-title">
          <strong>{{ statusTitle }}</strong>
          <span>{{ statusSubtitle }}</span>
        </div>
        <div class="toolbar-actions">
          <a-button :loading="loading" @click="loadReport">
            <template #icon>
              <a-icon-refresh />
            </template>
            检查
          </a-button>
          <a-button type="primary" status="warning" :loading="repairing" @click="repairConsistency">
            <template #icon>
              <a-icon-sync />
            </template>
            修复
          </a-button>
        </div>
      </section>

      <a-spin :loading="loading || repairing" class="consistency-spin">
        <a-alert v-if="errorMessage" type="error" :title="errorMessage" show-icon class="page-alert" />
        <a-alert
          v-else-if="report"
          :type="report.consistent ? 'success' : 'warning'"
          :title="report.consistent ? '三方数据一致' : '发现数据不一致'"
          show-icon
          class="page-alert"
        />

        <div class="summary-grid">
          <div class="summary-tile">
            <div class="summary-icon html-icon">
              <a-icon-file />
            </div>
            <div class="summary-content">
              <span>HTML 文件</span>
              <strong>{{ report?.htmlFiles ?? 0 }}</strong>
            </div>
          </div>
          <div class="summary-tile">
            <div class="summary-icon meili-icon">
              <a-icon-search />
            </div>
            <div class="summary-content">
              <span>搜索文档</span>
              <strong>{{ report?.meiliDocuments ?? 0 }}</strong>
            </div>
          </div>
          <div class="summary-tile">
            <div class="summary-icon db-icon">
              <a-icon-storage />
            </div>
            <div class="summary-content">
              <span>数据库统计</span>
              <strong>{{ report?.databaseStatTotal ?? 0 }}</strong>
            </div>
          </div>
          <div class="summary-tile">
            <div class="summary-icon recoverable-icon">
              <a-icon-tool />
            </div>
            <div class="summary-content">
              <span>可恢复</span>
              <strong>{{ recoverableIssues.length }}</strong>
            </div>
          </div>
          <div class="summary-tile">
            <div class="summary-icon unrecoverable-icon">
              <a-icon-exclamation-circle />
            </div>
            <div class="summary-content">
              <span>无法恢复</span>
              <strong>{{ unrecoverableIssues.length }}</strong>
            </div>
          </div>
        </div>

        <section v-if="reportActions.length" class="action-section">
          <div class="section-heading">
            <h2>修复动作</h2>
            <span>{{ reportActions.length }} 项</span>
          </div>
          <div class="action-list">
            <div v-for="action in reportActions" :key="action" class="action-row">
              <a-icon-check-circle />
              <span>{{ action }}</span>
            </div>
          </div>
        </section>

        <section class="issue-section">
          <div class="section-heading">
            <h2>可恢复问题</h2>
            <span>{{ recoverableIssues.length }} 项</span>
          </div>
          <a-empty v-if="recoverableIssues.length === 0" description="暂无可恢复问题" />
          <div v-else class="issue-list">
            <div v-for="issue in recoverableIssues" :key="issueKey(issue)" class="issue-row recoverable">
              <div class="issue-main">
                <div class="issue-title">
                  <a-tag color="blue">{{ storeLabel(issue.store) }}</a-tag>
                  <strong>{{ issue.message }}</strong>
                </div>
                <span class="issue-meta">{{ issueMeta(issue) }}</span>
              </div>
              <a-tag color="green">可自动处理</a-tag>
            </div>
          </div>
        </section>

        <section class="issue-section">
          <div class="section-heading">
            <h2>无法恢复</h2>
            <span>{{ unrecoverableIssues.length }} 项</span>
          </div>
          <a-empty v-if="unrecoverableIssues.length === 0" description="暂无无法恢复项目" />
          <div v-else class="issue-list">
            <div v-for="issue in unrecoverableIssues" :key="issueKey(issue)" class="issue-row unrecoverable">
              <div class="issue-main">
                <div class="issue-title">
                  <a-tag color="orangered">{{ storeLabel(issue.store) }}</a-tag>
                  <strong>{{ issue.message }}</strong>
                </div>
                <span class="issue-meta">{{ issueMeta(issue) }}</span>
              </div>
              <a-tag color="red">需要人工处理</a-tag>
            </div>
          </div>
        </section>
      </a-spin>
    </main>
  </div>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { Notification } from '@arco-design/web-vue'
import { useRouter } from 'vue-router'

interface ApiResponse<T = unknown> {
  Status: string
  Message: string
  Data?: T
  Error?: string
}

interface ConsistencyIssue {
  severity: string
  store: string
  domain: string
  filename: string
  path: string
  documentIds: string[]
  message: string
  recoverable: boolean
}

interface ConsistencyReport {
  checkedAt: string
  consistent: boolean
  htmlFiles: number
  meiliDocuments: number
  databaseStatTotal: number
  recoverableIssues: ConsistencyIssue[] | null
  unrecoverableIssues: ConsistencyIssue[] | null
  actions: string[] | null
  indexedDocuments: number
  refreshedStatSources: number
}

class ApiResponseError extends Error {
  constructor(
    message: string,
    readonly statusCode: number,
  ) {
    super(message)
  }
}

const router = useRouter()
const checkEndpoint = '/api/archiveConsistency'
const repairEndpoint = '/api/archiveConsistency/repair'

const report = ref<ConsistencyReport | null>(null)
const loading = ref(false)
const repairing = ref(false)
const errorMessage = ref('')

const statusTitle = computed(() => {
  if (!report.value) {
    return '等待检查'
  }
  return report.value.consistent ? '当前一致' : '存在偏差'
})

const statusSubtitle = computed(() => {
  if (!report.value) {
    return '最近状态尚未加载'
  }
  return `最后检查 ${new Date(report.value.checkedAt).toLocaleString()}`
})

const reportActions = computed(() => report.value?.actions ?? [])
const recoverableIssues = computed(() => report.value?.recoverableIssues ?? [])
const unrecoverableIssues = computed(() => report.value?.unrecoverableIssues ?? [])

const getAuthToken = () => {
  return localStorage.getItem('token') || sessionStorage.getItem('token')
}

const authHeaders = (): Record<string, string> => {
  const token = getAuthToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

const goBack = () => {
  router.push('/')
}

const redirectToLogin = () => {
  localStorage.removeItem('token')
  sessionStorage.removeItem('token')
  router.push('/login')
}

const parseResponse = async (response: Response): Promise<ApiResponse<ConsistencyReport>> => {
  let payload: ApiResponse<ConsistencyReport> | null = null
  try {
    payload = await response.json()
  } catch {
    payload = null
  }

  if (!response.ok || payload?.Status === '0') {
    throw new ApiResponseError(payload?.Error || payload?.Message || '一致性请求失败', response.status)
  }
  if (!payload?.Data) {
    throw new ApiResponseError(payload?.Message || '一致性响应缺少数据', response.status)
  }
  return payload
}

const requestReport = async (method: 'GET' | 'POST', url: string) => {
  const response = await fetch(url, {
    method,
    headers: authHeaders(),
  })
  const payload = await parseResponse(response)
  report.value = payload.Data ?? null
}

const handleRequestError = (error: unknown) => {
  if (error instanceof ApiResponseError && error.statusCode === 401) {
    redirectToLogin()
    return
  }

  errorMessage.value = error instanceof Error ? error.message : '一致性请求失败'
  Notification.error({
    title: '请求失败',
    content: errorMessage.value,
    position: 'topRight',
    duration: 5000,
  })
}

const loadReport = async () => {
  try {
    loading.value = true
    errorMessage.value = ''
    await requestReport('GET', checkEndpoint)
  } catch (error) {
    handleRequestError(error)
  } finally {
    loading.value = false
  }
}

const repairConsistency = async () => {
  try {
    repairing.value = true
    errorMessage.value = ''
    await requestReport('POST', repairEndpoint)
    Notification.success({
      title: '修复完成',
      content: '一致性修复已执行',
      position: 'topRight',
      duration: 3500,
    })
  } catch (error) {
    handleRequestError(error)
  } finally {
    repairing.value = false
  }
}

const storeLabel = (store: string) => {
  if (store === 'html') {
    return 'HTML'
  }
  if (store === 'meilisearch') {
    return 'Meilisearch'
  }
  if (store === 'database') {
    return '数据库'
  }
  return store
}

const issueMeta = (issue: ConsistencyIssue) => {
  const parts = [issue.domain, issue.filename, issue.path].filter(Boolean)
  if (issue.documentIds?.length) {
    parts.push(`文档 ${issue.documentIds.join(', ')}`)
  }
  return parts.length ? parts.join(' / ') : '无定位信息'
}

const issueKey = (issue: ConsistencyIssue) => {
  return [issue.store, issue.domain, issue.filename, issue.path, issue.message].join('|')
}

onMounted(() => {
  void loadReport()
})
</script>

<style lang="less" scoped>
.consistency-view {
  min-height: 100vh;
  padding: 20px;
  background:
    linear-gradient(180deg, rgba(241, 245, 249, 0.92) 0%, rgba(248, 250, 252, 1) 45%),
    #f8fafc;
  position: relative;
}

.back-button-container {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
}

.back-button {
  color: #4b5563;
  background: rgba(255, 255, 255, 0.88);
  border-radius: 8px;
  padding: 8px 14px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.08);

  &:hover {
    color: #0f766e;
    background: #ffffff;
  }
}

.consistency-container {
  width: min(1040px, 100%);
  margin: 0 auto;
  padding: 72px 0 40px;
}

.header-section {
  text-align: center;
  margin-bottom: 28px;
}

.icon-wrapper {
  width: 72px;
  height: 72px;
  border-radius: 18px;
  background: linear-gradient(135deg, #0f766e, #2563eb);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
  box-shadow: 0 16px 34px rgba(15, 118, 110, 0.2);
}

.header-icon {
  font-size: 36px;
  color: #ffffff;
}

.page-title {
  font-size: 32px;
  font-weight: 700;
  color: #111827;
  margin: 0 0 8px;
}

.page-subtitle {
  font-size: 16px;
  color: #64748b;
  margin: 0;
}

.toolbar-band {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border: 1px solid #dbe4ef;
  border-radius: 8px;
  background: #ffffff;
  padding: 18px 20px;
  box-shadow: 0 14px 34px rgba(15, 23, 42, 0.08);
}

.toolbar-title {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;

  strong {
    color: #0f172a;
    font-size: 18px;
  }

  span {
    color: #64748b;
    font-size: 14px;
  }
}

.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.consistency-spin {
  display: block;
  width: 100%;
  min-height: 360px;
}

.page-alert {
  margin-top: 18px;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 14px;
  margin-top: 18px;
}

.summary-tile {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  border: 1px solid #dbe4ef;
  border-radius: 8px;
  background: #ffffff;
  padding: 16px;
}

.summary-icon {
  display: grid;
  place-items: center;
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  border-radius: 8px;
  font-size: 21px;
}

.html-icon {
  color: #2563eb;
  background: #eff6ff;
}

.meili-icon {
  color: #7c3aed;
  background: #f5f3ff;
}

.db-icon {
  color: #0f766e;
  background: #ecfdf5;
}

.recoverable-icon {
  color: #d97706;
  background: #fffbeb;
}

.unrecoverable-icon {
  color: #dc2626;
  background: #fef2f2;
}

.summary-content {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;

  span {
    color: #64748b;
    font-size: 13px;
  }

  strong {
    color: #0f172a;
    font-size: 26px;
    line-height: 1.1;
  }
}

.action-section,
.issue-section {
  margin-top: 22px;
  border: 1px solid #dbe4ef;
  border-radius: 8px;
  background: #ffffff;
  padding: 20px;
}

.section-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;

  h2 {
    margin: 0;
    color: #111827;
    font-size: 20px;
    font-weight: 700;
  }

  span {
    color: #64748b;
    font-size: 14px;
  }
}

.action-list,
.issue-list {
  display: grid;
  gap: 12px;
}

.action-row,
.issue-row {
  border-radius: 8px;
  padding: 14px 16px;
}

.action-row {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #065f46;
  background: #ecfdf5;
  border: 1px solid #a7f3d0;
}

.issue-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  border: 1px solid #e2e8f0;
}

.issue-row.recoverable {
  background: #f8fafc;
}

.issue-row.unrecoverable {
  background: #fff7ed;
  border-color: #fed7aa;
}

.issue-main {
  min-width: 0;
  display: grid;
  gap: 8px;
}

.issue-title {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;

  strong {
    min-width: 0;
    color: #0f172a;
    font-size: 15px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
}

.issue-meta {
  color: #64748b;
  font-size: 13px;
  overflow-wrap: anywhere;
}

@media (max-width: 900px) {
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .consistency-view {
    padding: 16px;
  }

  .back-button-container {
    position: static;
    display: flex;
    justify-content: flex-end;
    margin-bottom: 8px;
  }

  .consistency-container {
    padding-top: 24px;
  }

  .toolbar-band,
  .issue-row {
    flex-direction: column;
    align-items: stretch;
  }

  .summary-grid {
    grid-template-columns: 1fr;
  }

  .page-title {
    font-size: 28px;
  }
}
</style>
