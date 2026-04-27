<template>
  <div class="stats-view">
    <div class="back-button-container">
      <a-button type="text" @click="goBack" class="back-button">
        <template #icon>
          <a-icon-arrow-left />
        </template>
        返回首页
      </a-button>
    </div>

    <div class="stats-container">
      <div class="header-section">
        <div class="icon-wrapper">
          <a-icon-bar-chart class="header-icon" />
        </div>
        <h1 class="page-title">项目统计</h1>
        <p class="page-subtitle">查看归档 HTML 总量和各 URL 来源分布</p>
      </div>

      <a-card class="stats-card" :bordered="false">
        <div class="toolbar">
          <div class="toolbar-title">
            <span class="toolbar-heading">归档概览</span>
            <span class="toolbar-subtitle">数据来自后端统计表</span>
          </div>
          <div class="toolbar-actions">
            <a-button :loading="loading" @click="loadStats" class="toolbar-button">
              <template #icon>
                <a-icon-refresh />
              </template>
              重新查询
            </a-button>
            <a-button type="primary" :loading="refreshing" @click="refreshStats" class="toolbar-button">
              <template #icon>
                <a-icon-sync />
              </template>
              刷新统计
            </a-button>
          </div>
        </div>

        <a-spin :loading="loading || refreshing" class="stats-spin">
          <a-alert v-if="errorMessage" type="error" :message="errorMessage" show-icon class="stats-alert" />

          <div class="summary-grid">
            <div class="summary-tile">
              <div class="summary-icon file-icon">
                <a-icon-file />
              </div>
              <div class="summary-content">
                <span class="summary-label">HTML 文件总数</span>
                <strong class="summary-value">{{ stats.totalFiles }}</strong>
              </div>
            </div>
            <div class="summary-tile">
              <div class="summary-icon source-icon">
                <a-icon-storage />
              </div>
              <div class="summary-content">
                <span class="summary-label">URL 来源数量</span>
                <strong class="summary-value">{{ stats.sources.length }}</strong>
              </div>
            </div>
            <div class="summary-tile">
              <div class="summary-icon top-icon">
                <a-icon-dashboard />
              </div>
              <div class="summary-content">
                <span class="summary-label">最多来源</span>
                <strong class="summary-value source-name">{{ topSourceLabel }}</strong>
              </div>
            </div>
          </div>

          <div class="source-section">
            <div class="section-header">
              <h2>来源分布</h2>
              <span>{{ stats.sources.length }} 个来源</span>
            </div>

            <a-empty v-if="stats.sources.length === 0 && !errorMessage" description="暂无统计数据" />

            <div v-else class="source-list">
              <div v-for="item in sortedSources" :key="item.source" class="source-row">
                <div class="source-main">
                  <div class="source-title">
                    <a-icon-link />
                    <span>{{ item.source }}</span>
                  </div>
                  <div class="source-progress">
                    <div class="source-progress-bar" :style="{ width: `${getPercentage(item.fileCount)}%` }"></div>
                  </div>
                </div>
                <div class="source-count">
                  <strong>{{ item.fileCount }}</strong>
                  <span>{{ getPercentage(item.fileCount) }}%</span>
                </div>
              </div>
            </div>
          </div>
        </a-spin>
      </a-card>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { Notification } from '@arco-design/web-vue'
import { useRouter } from 'vue-router'

interface ArchiveStatItem {
  source: string
  fileCount: number
}

interface ArchiveStats {
  totalFiles: number
  sources: ArchiveStatItem[]
}

interface StatsResponse {
  Status: string
  Message: string
  Data?: ArchiveStats
  Error?: string
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
const statsEndpoint = '/api/archiveStats'
const refreshStatsEndpoint = '/api/archiveStats/refresh'

const stats = reactive<ArchiveStats>({
  totalFiles: 0,
  sources: [],
})
const loading = ref(false)
const refreshing = ref(false)
const errorMessage = ref('')

const getAuthToken = () => {
  return localStorage.getItem('token') || sessionStorage.getItem('token')
}

const authHeaders = (): Record<string, string> => {
  const token = getAuthToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

const sortedSources = computed(() => {
  return [...stats.sources].sort((left, right) => {
    if (right.fileCount !== left.fileCount) {
      return right.fileCount - left.fileCount
    }
    return left.source.localeCompare(right.source)
  })
})

const topSourceLabel = computed(() => {
  const topSource = sortedSources.value[0]
  return topSource ? topSource.source : '-'
})

const goBack = () => {
  router.push('/')
}

const parseStatsResponse = async (response: Response): Promise<StatsResponse> => {
  let payload: StatsResponse | null = null

  try {
    payload = await response.json()
  } catch {
    payload = null
  }

  if (!response.ok || payload?.Status === '0') {
    throw new ApiResponseError(payload?.Message || '请求统计信息失败', response.status)
  }

  return payload || { Status: '1', Message: '', Data: { totalFiles: 0, sources: [] } }
}

const applyStats = (nextStats?: ArchiveStats) => {
  stats.totalFiles = nextStats?.totalFiles ?? 0
  stats.sources = nextStats?.sources ?? []
}

const handleRequestError = (error: unknown) => {
  if (error instanceof ApiResponseError && error.statusCode === 401) {
    localStorage.removeItem('token')
    sessionStorage.removeItem('token')
    router.push('/login')
    return
  }

  errorMessage.value = error instanceof Error ? error.message : '统计信息请求失败'
  Notification.error({
    title: '请求失败',
    content: errorMessage.value,
    position: 'topRight',
    duration: 5000,
  })
}

const requestStats = async (method: 'GET' | 'POST', url: string) => {
  const response = await fetch(url, {
    method,
    headers: authHeaders(),
  })
  return parseStatsResponse(response)
}

const loadStats = async () => {
  try {
    loading.value = true
    errorMessage.value = ''
    const payload = await requestStats('GET', statsEndpoint)
    applyStats(payload.Data)
  } catch (error) {
    handleRequestError(error)
  } finally {
    loading.value = false
  }
}

const refreshStats = async () => {
  try {
    refreshing.value = true
    errorMessage.value = ''
    // 刷新统计会触发后端扫描归档目录，所以只在用户主动点击时执行。
    const payload = await requestStats('POST', refreshStatsEndpoint)
    applyStats(payload.Data)
    Notification.success({
      title: '刷新完成',
      content: '统计信息已根据归档目录重新生成',
      position: 'topRight',
      duration: 3000,
    })
  } catch (error) {
    handleRequestError(error)
  } finally {
    refreshing.value = false
  }
}

const getPercentage = (fileCount: number) => {
  if (stats.totalFiles <= 0) {
    return 0
  }
  return Math.round((fileCount / stats.totalFiles) * 100)
}

onMounted(() => {
  void loadStats()
})
</script>

<style lang="less" scoped>
.stats-view {
  min-height: 100vh;
  padding: 20px;
  background:
    linear-gradient(180deg, rgba(242, 247, 255, 0.9) 0%, rgba(248, 250, 252, 1) 42%),
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
    color: #1d4ed8;
    background: #ffffff;
  }
}

.stats-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  max-width: 960px;
  margin: 0 auto;
  padding-top: 72px;
}

.header-section {
  text-align: center;
  margin-bottom: 32px;
}

.icon-wrapper {
  width: 72px;
  height: 72px;
  border-radius: 18px;
  background: linear-gradient(135deg, #2563eb, #059669);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
  box-shadow: 0 16px 34px rgba(37, 99, 235, 0.22);
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
  line-height: 1.6;
}

.stats-card {
  width: 100%;
  border: 1px solid rgba(203, 213, 225, 0.72);
  border-radius: 8px;
  box-shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
  background: rgba(255, 255, 255, 0.94);
  padding: 24px;

  :deep(.arco-card-body) {
    padding: 0;
  }
}

.toolbar {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  padding-bottom: 24px;
  border-bottom: 1px solid #e2e8f0;
}

.toolbar-title {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.toolbar-heading {
  font-size: 20px;
  font-weight: 700;
  color: #111827;
}

.toolbar-subtitle {
  color: #64748b;
  font-size: 14px;
}

.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.toolbar-button {
  border-radius: 8px;
  font-weight: 600;
}

.stats-spin {
  display: block;
  width: 100%;
  min-height: 260px;
}

.stats-alert {
  margin-top: 24px;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
  margin-top: 28px;
}

.summary-tile {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  padding: 18px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #f8fafc;
}

.summary-icon {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex: 0 0 44px;
  border-radius: 10px;
  font-size: 22px;
}

.file-icon {
  color: #2563eb;
  background: #eff6ff;
}

.source-icon {
  color: #059669;
  background: #ecfdf5;
}

.top-icon {
  color: #d46b08;
  background: #fff4e8;
}

.summary-content {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.summary-label {
  color: #64748b;
  font-size: 14px;
}

.summary-value {
  color: #0f172a;
  font-size: 28px;
  line-height: 1.1;
}

.source-name {
  font-size: 20px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.source-section {
  margin-top: 32px;
}

.section-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;

  h2 {
    margin: 0;
    color: #111827;
    font-size: 20px;
  }

  span {
    color: #64748b;
  }
}

.source-list {
  display: grid;
  gap: 12px;
}

.source-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 92px;
  gap: 18px;
  align-items: center;
  padding: 16px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #ffffff;
}

.source-main {
  min-width: 0;
  display: grid;
  gap: 10px;
}

.source-title {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #334155;
  font-weight: 600;

  span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.source-progress {
  height: 8px;
  border-radius: 999px;
  overflow: hidden;
  background: #e2e8f0;
}

.source-progress-bar {
  height: 100%;
  min-width: 4px;
  border-radius: inherit;
  background: #2563eb;
  transition: width 0.25s ease;
}

.source-count {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;

  strong {
    color: #0f172a;
    font-size: 22px;
    line-height: 1.1;
  }

  span {
    color: #64748b;
    font-size: 13px;
  }
}

@media (max-width: 720px) {
  .stats-view {
    padding: 16px;
  }

  .back-button-container {
    position: static;
    display: flex;
    justify-content: flex-end;
    margin-bottom: 8px;
  }

  .stats-container {
    padding-top: 24px;
  }

  .page-title {
    font-size: 28px;
  }

  .stats-card {
    padding: 18px;
  }

  .toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .toolbar-actions {
    justify-content: flex-start;
    width: 100%;
  }

  .summary-grid {
    grid-template-columns: 1fr;
  }

  .source-row {
    grid-template-columns: 1fr;
    gap: 12px;
  }

  .source-count {
    align-items: flex-start;
    flex-direction: row;
  }
}

@media (max-width: 480px) {
  .stats-card {
    padding: 18px;
  }

  .toolbar-button {
    width: 100%;
  }
}
</style>
