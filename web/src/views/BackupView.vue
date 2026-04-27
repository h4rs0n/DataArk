<template>
  <div class="backup-view">
    <div class="back-button-container">
      <a-button type="text" @click="goBack" class="back-button">
        <template #icon>
          <a-icon-arrow-left />
        </template>
        返回首页
      </a-button>
    </div>

    <main class="backup-container">
      <div class="header-section">
        <div class="icon-wrapper">
          <a-icon-storage class="header-icon" />
        </div>
        <h1 class="page-title">数据备份</h1>
        <p class="page-subtitle">导出与恢复归档数据</p>
      </div>

      <section class="operation-section">
        <div class="section-heading">
          <div>
            <h2>备份</h2>
            <span>生成 Meilisearch、PostgreSQL 和 archive 的完整压缩包</span>
          </div>
          <a-tag color="green">zip</a-tag>
        </div>

        <div class="operation-panel">
          <div class="operation-copy">
            <strong>导出当前数据</strong>
            <span>{{ backupStatusText }}</span>
          </div>
          <a-button type="primary" size="large" :loading="backingUp" @click="downloadBackup">
            <template #icon>
              <a-icon-download />
            </template>
            {{ backingUp ? '备份中...' : '开始备份' }}
          </a-button>
        </div>

        <a-progress
          v-if="backingUp"
          :percent="backupProgress"
          :show-text="false"
          status="normal"
          class="operation-progress"
        />
      </section>

      <section class="operation-section restore-section">
        <div class="section-heading">
          <div>
            <h2>恢复</h2>
            <span>上传备份压缩包并覆盖当前数据</span>
          </div>
          <a-tag color="orangered">覆盖</a-tag>
        </div>

        <a-alert
          type="warning"
          show-icon
          class="restore-alert"
          title="恢复会覆盖当前数据库、归档文件和搜索索引"
        />

        <a-spin :loading="restoring" class="restore-spin">
          <a-upload
            draggable
            accept=".zip"
            :limit="1"
            :auto-upload="false"
            v-model:file-list="restoreFiles"
            class="restore-upload"
            @change="handleRestoreFileChange"
          >
            <template #upload-button>
              <div class="upload-zone">
                <a-icon-upload class="upload-icon" />
                <strong>{{ restoring ? '正在恢复备份' : '选择备份压缩包' }}</strong>
                <span>{{ restoreStatusText }}</span>
              </div>
            </template>
          </a-upload>
        </a-spin>
      </section>
    </main>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { Modal, Notification } from '@arco-design/web-vue'
import { useRouter } from 'vue-router'

interface ApiResponse<T = unknown> {
  Status: string
  Message: string
  Data?: T
  Error?: string
}

interface RestoreResult {
  meiliDumpFile: string
  databaseRestored: boolean
  archiveRestored: boolean
  indexedDocuments: number
  refreshedStatRows: number
}

const router = useRouter()
const backupEndpoint = '/api/backup'
const restoreEndpoint = '/api/backup/restore'

const backingUp = ref(false)
const restoring = ref(false)
const restorePromptOpen = ref(false)
const restoreFiles = ref<any[]>([])

const backupStatusText = computed(() => {
  return backingUp.value ? '正在创建备份并准备下载' : '点击后会自动下载备份文件'
})

const restoreStatusText = computed(() => {
  return restoring.value ? '请保持页面打开，恢复完成后会自动提示' : '支持 DataArk 导出的 .zip 文件'
})

const backupProgress = computed(() => {
  return backingUp.value ? 0.72 : 0
})

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

const readJsonResponse = async <T>(response: Response): Promise<ApiResponse<T>> => {
  try {
    return await response.json()
  } catch {
    return { Status: response.ok ? '1' : '0', Message: response.ok ? '' : '请求失败' }
  }
}

const getErrorMessage = async (response: Response, fallback: string) => {
  const payload = await readJsonResponse(response)
  return payload.Error || payload.Message || fallback
}

const parseDownloadFileName = (disposition: string | null) => {
  const fallback = `dataark-backup-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.zip`
  if (!disposition) {
    return fallback
  }

  const match = disposition.match(/filename="?([^"]+)"?/i)
  return match?.[1] || fallback
}

const triggerBlobDownload = (blob: Blob, fileName: string) => {
  const downloadUrl = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = downloadUrl
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(downloadUrl)
}

const downloadBackup = async () => {
  try {
    backingUp.value = true
    const response = await fetch(backupEndpoint, {
      method: 'POST',
      headers: authHeaders(),
    })

    if (response.status === 401) {
      redirectToLogin()
      return
    }
    if (!response.ok) {
      throw new Error(await getErrorMessage(response, '创建备份失败'))
    }

    const blob = await response.blob()
    triggerBlobDownload(blob, parseDownloadFileName(response.headers.get('Content-Disposition')))

    Notification.success({
      title: '备份完成',
      content: '备份文件已开始下载',
      position: 'topRight',
      duration: 3500,
    })
  } catch (error) {
    Notification.error({
      title: '备份失败',
      content: error instanceof Error ? error.message : '创建备份失败',
      position: 'topRight',
      duration: 5000,
    })
  } finally {
    backingUp.value = false
  }
}

const resolveRawFile = (fileItem: any): File | null => {
  const rawFile = fileItem?.file || fileItem?.originFile || fileItem
  return rawFile instanceof File ? rawFile : null
}

const handleRestoreFileChange = (fileList: any[], fileItem?: any) => {
  if (restoring.value || restorePromptOpen.value || fileList.length === 0) {
    return
  }

  const selectedFile = resolveRawFile(fileItem || fileList[fileList.length - 1])
  if (!selectedFile) {
    restoreFiles.value = []
    return
  }

  restorePromptOpen.value = true
  Modal.confirm({
    title: '确认恢复备份',
    content: '恢复会覆盖当前已有数据，操作开始后请等待完成。',
    okText: '确认恢复',
    cancelText: '取消',
    onOk: () => {
      restorePromptOpen.value = false
      void restoreBackup(selectedFile)
    },
    onCancel: () => {
      restorePromptOpen.value = false
      restoreFiles.value = []
    },
  })
}

const restoreBackup = async (file: File) => {
  try {
    restoring.value = true
    const formData = new FormData()
    formData.append('file', file)

    const response = await fetch(restoreEndpoint, {
      method: 'POST',
      headers: authHeaders(),
      body: formData,
    })

    if (response.status === 401) {
      redirectToLogin()
      return
    }

    const payload = await readJsonResponse<RestoreResult>(response)
    if (!response.ok || payload.Status === '0') {
      throw new Error(payload.Error || payload.Message || '恢复备份失败')
    }

    Notification.success({
      title: '恢复完成',
      content: `已重建 ${payload.Data?.indexedDocuments ?? 0} 条搜索记录`,
      position: 'topRight',
      duration: 4500,
    })
  } catch (error) {
    Notification.error({
      title: '恢复失败',
      content: error instanceof Error ? error.message : '恢复备份失败',
      position: 'topRight',
      duration: 6000,
    })
  } finally {
    restoring.value = false
    restoreFiles.value = []
  }
}
</script>

<style lang="less" scoped>
.backup-view {
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

.backup-container {
  width: min(960px, 100%);
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
}

.operation-section {
  background: rgba(255, 255, 255, 0.94);
  border: 1px solid rgba(203, 213, 225, 0.72);
  border-radius: 8px;
  padding: 24px;
  box-shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
}

.restore-section {
  margin-top: 20px;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;

  h2 {
    margin: 0 0 6px;
    color: #111827;
    font-size: 20px;
    line-height: 1.3;
  }

  span {
    color: #64748b;
    font-size: 14px;
  }
}

.operation-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 18px;
  border-radius: 8px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
}

.operation-copy {
  display: flex;
  flex-direction: column;
  gap: 6px;

  strong {
    color: #0f172a;
    font-size: 16px;
  }

  span {
    color: #64748b;
    font-size: 14px;
  }
}

.operation-progress {
  margin-top: 16px;
}

.restore-alert {
  margin-bottom: 16px;
}

.restore-spin {
  width: 100%;
}

.restore-upload {
  width: 100%;
}

.upload-zone {
  min-height: 176px;
  border: 1px dashed #94a3b8;
  border-radius: 8px;
  background: #f8fafc;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: #334155;
  transition: border-color 0.2s ease, background 0.2s ease;

  &:hover {
    border-color: #2563eb;
    background: #eff6ff;
  }

  strong {
    font-size: 16px;
  }

  span {
    color: #64748b;
    font-size: 14px;
  }
}

.upload-icon {
  font-size: 34px;
  color: #2563eb;
}

@media (max-width: 720px) {
  .backup-view {
    padding: 16px;
  }

  .back-button-container {
    position: static;
    display: flex;
    justify-content: flex-end;
    margin-bottom: 8px;
  }

  .backup-container {
    padding-top: 24px;
  }

  .page-title {
    font-size: 28px;
  }

  .operation-section {
    padding: 18px;
  }

  .section-heading,
  .operation-panel {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
