<template>
  <div class="authenticated-html-viewer">
    <!-- 加载状态 -->
    <div v-if="loading" class="loading-container">
      <a-spin size="large" :style="{ color: '#1890ff' }">
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
        <a-space>
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

<script setup>
import { ref, onMounted, computed } from 'vue'
import {useRoute, useRouter} from 'vue-router'
import { Message } from '@arco-design/web-vue'
import {IconArrowLeft, IconRefresh} from '@arco-design/web-vue/es/icon'

const router = useRouter();
// 响应式数据
const loading = ref(false)
const htmlContent = ref('')
const error = ref(null)
const route = useRoute()

// 从路由参数获取路径
const currentPath = computed(() => {
  return route.query.loc || ''
})

// 从localStorage或其他地方获取token
const getAuthToken = () => {
  // 这里可以根据您的实际情况获取token
  // 例如从localStorage、vuex、pinia等
  return localStorage.getItem('token') ||
      sessionStorage.getItem('token') ||
      process.env.VUE_APP_AUTH_TOKEN
}

// 返回功能
const goBack = () => {
  // 优先使用浏览器历史记录返回
  if (window.history.length > 1) {
    router.go(-1)
  } else {
    // 如果没有历史记录，返回到默认页面
    router.push('/')
  }
}

// 加载HTML资源
const loadHtmlResource = async (path) => {
  if (!path) {
    error.value = {
      title: '参数错误',
      message: '请提供有效的资源路径参数 loc'
    }
    return
  }

  const token = getAuthToken()
  if (!token) {
    error.value = {
      title: '认证失败',
      message: '未找到有效的认证token，请先登录'
    }
    return
  }

  loading.value = true
  error.value = null

  try {
    const response = await fetch(path, {
      method: 'GET',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'text/html',
        'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'
      },
      credentials: 'include' // 如果需要发送cookie
    })

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`)
    }

    const html = await response.text()
    htmlContent.value = html

    Message.success('HTML资源加载成功')

  } catch (err) {
    console.error('加载HTML资源失败:', err)

    error.value = {
      title: '加载失败',
      message: `无法加载HTML资源: ${err.message}`
    }

    Message.error(`加载失败: ${err.message}`)
  } finally {
    loading.value = false
  }
}

// 重新加载
const retryLoad = () => {
  loadHtmlResource(currentPath.value)
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