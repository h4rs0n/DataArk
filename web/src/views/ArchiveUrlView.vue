<template>
  <div class="index-view">
    <div class="back-button-container">
      <a-button type="text" @click="goBack" class="back-button">
        <template #icon>
          <a-icon-arrow-left />
        </template>
        返回首页
      </a-button>
    </div>

    <div class="archive-container">
      <div class="header-section">
        <div class="icon-wrapper">
          <a-icon-link class="header-icon" />
        </div>
        <h1 class="page-title">使用 URL 保存 HTML</h1>
        <p class="page-subtitle">提交网页链接后，系统会生成可搜索的离线归档</p>
      </div>

      <a-card class="archive-card" :bordered="false">
        <a-form :model="formData" @submit="handleSubmit" layout="vertical">
          <a-form-item
            field="url"
            label="网页 URL"
            validate-trigger="blur"
            :rules="[{ required: true, message: '请输入网页 URL' }]"
            class="form-item-enhanced"
          >
            <a-input
              v-model="formData.url"
              placeholder="https://example.com/article"
              size="large"
              class="input-enhanced"
            >
              <template #prefix>
                <a-icon-link />
              </template>
            </a-input>
          </a-form-item>

          <div class="form-actions">
            <a-button
              type="primary"
              html-type="submit"
              :loading="submitting || polling"
              size="large"
              class="submit-button"
            >
              <template #icon>
                <a-icon-check />
              </template>
              {{ submitting || polling ? '处理中...' : '开始保存' }}
            </a-button>
          </div>
        </a-form>

        <div v-if="currentTask" class="task-panel">
          <a-alert :type="statusMeta.alertType" :message="statusMeta.title" show-icon>
            <template #icon>
              <a-icon-check v-if="currentTask.status === 'success'" />
              <a-icon-exclamation-circle v-else-if="currentTask.status === 'failed'" />
              <a-icon-clock-circle v-else />
            </template>
            <template #description>
              <div class="task-description">
                <div>{{ statusMeta.description }}</div>
                <div v-if="currentTask.error" class="task-error">{{ currentTask.error }}</div>
              </div>
            </template>
          </a-alert>

          <div class="task-meta">
            <div class="task-meta-item">
              <span class="task-meta-label">任务编号</span>
              <span class="task-meta-value">{{ currentTask.id }}</span>
            </div>
            <div class="task-meta-item">
              <span class="task-meta-label">域名</span>
              <span class="task-meta-value">{{ currentTask.domain || '-' }}</span>
            </div>
            <div class="task-meta-item">
              <span class="task-meta-label">文件</span>
              <span class="task-meta-value">{{ currentTask.fileName || '-' }}</span>
            </div>
          </div>

          <div class="task-actions">
            <a-button
              type="primary"
              :disabled="!archiveFilePath"
              @click="viewArchive"
              class="task-button"
            >
              <template #icon>
                <a-icon-eye />
              </template>
              查看归档
            </a-button>
            <a-button
              :loading="polling"
              :disabled="!currentTask.id || currentTask.status === 'success'"
              @click="refreshTaskStatus"
              class="task-button"
            >
              <template #icon>
                <a-icon-refresh />
              </template>
              刷新状态
            </a-button>
          </div>
        </div>
      </a-card>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { Notification } from '@arco-design/web-vue'
import { useRouter } from 'vue-router'

type ArchiveTaskStatus = 'pending' | 'running' | 'success' | 'failed' | string

interface ArchiveTask {
  id: string
  url: string
  domain: string
  status: ArchiveTaskStatus
  fileName: string
  error: string
  externalTaskId: string
  createdAt: string
  updatedAt: string
  startedAt: string | null
  finishedAt: string | null
}

interface ArchiveTaskResponse {
  Status: string
  Message: string
  Data?: ArchiveTask
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
const archiveByUrlEndpoint = '/api/archiveByURL'
const archiveTaskEndpoint = '/api/archiveTask'
const pollInterval = 2000

const formData = reactive({
  url: '',
})

const currentTask = ref<ArchiveTask | null>(null)
const submitting = ref(false)
const polling = ref(false)
let pollingTimer: ReturnType<typeof window.setTimeout> | null = null

const getAuthToken = () => {
  return localStorage.getItem('token') || sessionStorage.getItem('token')
}

const authHeaders = (): Record<string, string> => {
  const token = getAuthToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

const isFinalStatus = (status: ArchiveTaskStatus) => {
  return status === 'success' || status === 'failed'
}

const archiveFilePath = computed(() => {
  if (!currentTask.value || currentTask.value.status !== 'success' || !currentTask.value.fileName) {
    return ''
  }

  return `/archive/${currentTask.value.domain}/${currentTask.value.fileName}`
})

const statusMeta = computed(() => {
  const status = currentTask.value?.status

  if (status === 'success') {
    return {
      alertType: 'success' as const,
      title: '链接离线任务已完成',
      description: '归档文件已生成，可以查看或在搜索中使用。',
    }
  }

  if (status === 'failed') {
    return {
      alertType: 'error' as const,
      title: '链接离线任务执行失败',
      description: '请检查链接是否可以访问，或稍后重试。',
    }
  }

  if (status === 'running') {
    return {
      alertType: 'info' as const,
      title: '链接离线任务正在处理中',
      description: '系统正在抓取网页并生成 HTML 归档。',
    }
  }

  return {
    alertType: 'info' as const,
    title: '链接离线任务已加入队列',
    description: '任务正在等待处理。',
  }
})

const clearPollingTimer = () => {
  if (pollingTimer !== null) {
    window.clearTimeout(pollingTimer)
    pollingTimer = null
  }
}

const goBack = () => {
  router.push('/')
}

const parseResponse = async (response: Response): Promise<ArchiveTaskResponse> => {
  let payload: ArchiveTaskResponse | null = null

  try {
    payload = await response.json()
  } catch {
    payload = null
  }

  if (!response.ok || payload?.Status === '0') {
    throw new ApiResponseError(payload?.Message || '请求失败', response.status)
  }

  if (!payload?.Data) {
    throw new ApiResponseError(payload?.Message || '任务信息缺失', response.status)
  }

  return payload
}

const requestArchiveByURL = async (url: string) => {
  const response = await fetch(archiveByUrlEndpoint, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
    },
    body: JSON.stringify({ url }),
  })

  return parseResponse(response)
}

const requestTaskStatus = async (taskId: string) => {
  const response = await fetch(`${archiveTaskEndpoint}/${encodeURIComponent(taskId)}`, {
    method: 'GET',
    headers: authHeaders(),
  })

  return parseResponse(response)
}

const notifyTaskResult = (task: ArchiveTask) => {
  if (task.status === 'success') {
    Notification.success({
      title: '保存完成',
      content: '网页已成功保存为 HTML 归档',
      position: 'topRight',
      duration: 4000,
    })
    return
  }

  if (task.status === 'failed') {
    Notification.error({
      title: '保存失败',
      content: task.error || '离线归档任务执行失败',
      position: 'topRight',
      duration: 5000,
    })
  }
}

const schedulePolling = () => {
  clearPollingTimer()

  if (!currentTask.value || isFinalStatus(currentTask.value.status)) {
    polling.value = false
    return
  }

  // archiveByURL returns 202 for pending/running tasks, so the UI must keep polling until a final status.
  polling.value = true
  pollingTimer = window.setTimeout(() => {
    void refreshTaskStatus()
  }, pollInterval)
}

const updateTask = (task: ArchiveTask) => {
  currentTask.value = task

  if (isFinalStatus(task.status)) {
    polling.value = false
    clearPollingTimer()
    notifyTaskResult(task)
    return
  }

  schedulePolling()
}

const redirectToLogin = () => {
  localStorage.removeItem('token')
  sessionStorage.removeItem('token')
  Notification.warning({
    title: '登录状态已过期',
    content: '请重新登录后再保存网页',
    position: 'topRight',
    duration: 4000,
  })
  router.push('/login')
}

const handleRequestError = (error: unknown) => {
  if (error instanceof ApiResponseError && error.statusCode === 401) {
    redirectToLogin()
    return
  }

  Notification.error({
    title: '请求失败',
    content: error instanceof Error ? error.message : '请求过程中发生错误，请稍后重试',
    position: 'topRight',
    duration: 5000,
  })
}

const isValidArchiveURL = (value: string) => {
  try {
    const parsedURL = new URL(value)
    return parsedURL.protocol === 'http:' || parsedURL.protocol === 'https:'
  } catch {
    return false
  }
}

const handleSubmit = async () => {
  const archiveURL = formData.url.trim()

  if (!isValidArchiveURL(archiveURL)) {
    Notification.error({
      title: '链接格式错误',
      content: '请输入以 http:// 或 https:// 开头的网页 URL',
      position: 'topRight',
      duration: 4000,
    })
    return
  }

  try {
    clearPollingTimer()
    submitting.value = true
    polling.value = false
    currentTask.value = null

    const payload = await requestArchiveByURL(archiveURL)
    if (payload.Message) {
      Notification.info({
        title: '任务已提交',
        content: payload.Message,
        position: 'topRight',
        duration: 3000,
      })
    }
    updateTask(payload.Data!)
  } catch (error) {
    handleRequestError(error)
  } finally {
    submitting.value = false
  }
}

const refreshTaskStatus = async () => {
  if (!currentTask.value?.id) {
    return
  }

  try {
    polling.value = true
    const payload = await requestTaskStatus(currentTask.value.id)
    updateTask(payload.Data!)
  } catch (error) {
    polling.value = false
    clearPollingTimer()
    handleRequestError(error)
  }
}

const viewArchive = () => {
  if (!archiveFilePath.value) {
    return
  }

  router.push({ path: '/htmlviewer', query: { loc: archiveFilePath.value } })
}

onBeforeUnmount(() => {
  clearPollingTimer()
})
</script>

<style lang="less" scoped>
.index-view {
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

.archive-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  max-width: 800px;
  margin: 0 auto;
  padding: 72px 0 40px;
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

.archive-card {
  width: 100%;
  max-width: 600px;
  border: 1px solid rgba(203, 213, 225, 0.72);
  border-radius: 8px;
  box-shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
  background: rgba(255, 255, 255, 0.94);
  padding: 24px;

  :deep(.arco-card-body) {
    padding: 0;
  }
}

.form-item-enhanced {
  margin-bottom: 32px;

  :deep(.arco-form-item-label) {
    font-weight: 600;
    color: #0f172a;
    font-size: 16px;
    margin-bottom: 8px;
  }
}

.input-enhanced {
  border-radius: 8px;
  border: 1px solid #e2e8f0;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;

  &:hover {
    border-color: #2563eb;
  }

  &:focus-within {
    border-color: #2563eb;
    box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
  }

  :deep(.arco-input) {
    border: none;
    font-size: 16px;
  }

  :deep(.arco-input-prefix) {
    color: #2563eb;
  }
}

.form-actions {
  text-align: center;
  margin-top: 40px;
}

.submit-button {
  padding: 12px 48px;
  height: auto;
  font-size: 16px;
  font-weight: 600;
  border-radius: 8px;
}

.task-panel {
  margin-top: 32px;
  padding-top: 28px;
  border-top: 1px solid #e2e8f0;
}

.task-description {
  line-height: 1.6;
}

.task-error {
  margin-top: 6px;
  color: #cb2634;
  word-break: break-word;
}

.task-meta {
  display: grid;
  gap: 12px;
  margin-top: 20px;
}

.task-meta-item {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  align-items: start;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #f8fafc;
}

.task-meta-label {
  color: #64748b;
  font-weight: 600;
}

.task-meta-value {
  color: #334155;
  word-break: break-all;
}

.task-actions {
  display: flex;
  justify-content: center;
  gap: 12px;
  margin-top: 24px;
  flex-wrap: wrap;
}

.task-button {
  min-width: 120px;
  border-radius: 8px;
  font-weight: 600;
}

@media (max-width: 720px) {
  .index-view {
    padding: 16px;
  }

  .back-button-container {
    position: static;
    display: flex;
    justify-content: flex-end;
    margin-bottom: 8px;
  }

  .archive-container {
    padding-top: 24px;
  }

  .page-title {
    font-size: 28px;
  }

  .archive-card {
    padding: 18px;
  }

  .submit-button {
    padding: 10px 32px;
    font-size: 14px;
  }
}

@media (max-width: 480px) {
  .archive-card {
    padding: 18px;
  }

  .form-item-enhanced {
    margin-bottom: 24px;
  }

  .task-meta-item {
    grid-template-columns: 1fr;
    gap: 4px;
  }
}
</style>
