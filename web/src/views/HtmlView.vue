<template>
  <div class="authenticated-html-viewer">
    <!-- 加载状态 -->
    <div v-if="loading" class="loading-container">
      <a-spin :style="{ color: '#1890ff' }">
        <template #element>
          <div class="custom-loading">
            <div class="loading-spinner"></div>
          </div>
        </template>
        <div class="loading-text">正在加载HTML资源...</div>
      </a-spin>
    </div>

    <!-- 错误状态 -->
    <div v-else-if="error" class="error-container">
      <a-result
          status="error"
          :title="error.title"
          :sub-title="error.message"
      >
        <template #extra>
          <a-button type="primary" @click="retryLoad">
            重新加载
          </a-button>
        </template>
      </a-result>
    </div>

    <!-- HTML内容展示 -->
    <div v-else class="html-content-container">
      <div class="content-header">
        <a-space wrap>
          <a-button
              type="primary"
              size="small"
              @click="retryLoad"
          >
            <template #icon>
              <IconRefresh />
            </template>
            刷新
          </a-button>
          <a-button
              size="small"
              @click="goBack"
          >
            <template #icon>
              <IconArrowLeft />
            </template>
            返回
          </a-button>
          <a-button
              status="danger"
              size="small"
              :loading="deleting"
              @click="confirmDelete"
          >
            <template #icon>
              <IconDelete />
            </template>
            删除
          </a-button>
          <a-tag color="blue">{{ currentPath }}</a-tag>
        </a-space>
      </div>

      <!-- 使用iframe显示HTML内容 -->
      <div class="html-viewer">
        <iframe
            ref="htmlFrame"
            :srcdoc="htmlContent"
            class="html-iframe"
            frameborder="0"
            sandbox="allow-scripts allow-same-origin"
        ></iframe>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Message, Modal } from '@arco-design/web-vue'
import { IconArrowLeft, IconDelete, IconRefresh } from '@arco-design/web-vue/es/icon'
import { requestText } from '@/api/client'
import { deleteArchiveDocument, recordArchiveClick } from '@/api/archive'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const deleting = ref(false)
const htmlContent = ref('')
const error = ref<{ title: string; message: string } | null>(null)
const route = useRoute()

const currentPath = computed(() => String(route.query.loc || ''))

const goBack = () => {
  if (window.history.length > 1) {
    router.go(-1)
  } else {
    router.push('/')
  }
}

const loadHtmlResource = async (path: string) => {
  if (!path) {
    error.value = {
      title: '参数错误',
      message: '请提供有效的资源路径参数 loc'
    }
    return
  }

  if (!auth.token) {
    error.value = {
      title: '认证失败',
      message: '未找到有效的认证token，请先登录'
    }
    return
  }

  loading.value = true
  error.value = null

  try {
    htmlContent.value = await requestText(path, {
      headers: {
        Accept: 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
      },
    })
    try {
      await recordArchiveClick(path, String(route.query.q || ''))
    } catch (err) {
      console.warn('记录归档点击失败:', err)
    }
    Message.success('HTML资源加载成功')
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '无法加载HTML资源'
    console.error('加载HTML资源失败:', err)
    error.value = {
      title: '加载失败',
      message: `无法加载HTML资源: ${message}`
    }
    Message.error(`加载失败: ${message}`)
  } finally {
    loading.value = false
  }
}

const retryLoad = () => {
  loadHtmlResource(currentPath.value)
}

const deleteHtmlResource = async () => {
  if (!currentPath.value) {
    Message.error('缺少待删除的 HTML 路径')
    return
  }

  if (!auth.token) {
    Message.error('未找到有效的认证token，请先登录')
    router.push('/login')
    return
  }

  deleting.value = true
  try {
    const payload = await deleteArchiveDocument(currentPath.value)
    Message.success(payload.Message || '文档删除成功')
    goBack()
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '删除失败'
    console.error('删除HTML资源失败:', err)
    Message.error(`删除失败: ${message}`)
  } finally {
    deleting.value = false
  }
}

const confirmDelete = () => {
  Modal.confirm({
    title: '确认删除文档',
    content: `确定删除 ${currentPath.value}？删除后将同时移除搜索记录和 HTML 文件。`,
    okText: '删除',
    cancelText: '取消',
    okButtonProps: {
      status: 'danger'
    },
    onOk: deleteHtmlResource
  })
}

// 组件挂载时加载资源
onMounted(() => {
  loadHtmlResource(currentPath.value)
})
</script>

<style scoped>
.authenticated-html-viewer {
  width: 100%;
  height: 100vh;
  display: flex;
  flex-direction: column;
}

.loading-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100vh;
  flex-direction: column;
}

.custom-loading {
  width: 50px;
  height: 50px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.loading-spinner {
  width: 40px;
  height: 40px;
  border: 4px solid #f0f0f0;
  border-top: 4px solid #1890ff;
  border-radius: 50%;
  animation: spin 1s linear infinite;
}

@keyframes spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}

.loading-text {
  margin-top: 16px;
  color: #1890ff;
  font-size: 16px;
}

.error-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100vh;
}

.html-content-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
}

.content-header {
  padding: 16px;
  border-bottom: 1px solid #e8e8e8;
  background: #fafafa;
}

.html-viewer {
  flex: 1;
  overflow: hidden;
}

.html-iframe {
  width: 100%;
  height: 100%;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .content-header {
    padding: 12px;
  }

  .loading-text {
    font-size: 14px;
  }
}
</style>
