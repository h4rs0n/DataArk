<template>
  <div class="search-box">
    <div class="search-input-wrap">
      <a-input-search
          size="large"
          v-model="pageData.searchKey"
          class="search-input"
          placeholder="输入搜索内容"
          :input-attrs="{ id: 'archive-search-input', name: 'archive-search', 'aria-label': '归档搜索' }"
          @search="getSearch"
          @keydown.enter.native="getSearch"
          @input="loadSuggestions"
          @focus="loadSuggestions"
          search-button
      />
      <div v-if="suggestions.length > 0" class="suggestions-panel">
        <button
            v-for="item in suggestions"
            :key="item.keyword"
            class="suggestion-item"
            type="button"
            @mousedown.prevent="chooseSuggestion(item.keyword)"
        >
          <span>{{ item.keyword }}</span>
          <small>{{ item.count }} 次</small>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Message } from '@arco-design/web-vue'
import { useRoute, useRouter } from 'vue-router'
import { onMounted, reactive, ref } from 'vue'

const pageData = reactive({ searchKey: '' })
const suggestions = ref<Array<{ keyword: string; count: number }>>([])
let suggestionTimer = 0

const route = useRoute()
const router = useRouter()

const getSearch = () => {
  if (pageData.searchKey === '' ){
    Message.warning('请输入需要搜索的内容')
  }
  if (!pageData.searchKey.trim()) return
  suggestions.value = []
  router.push({ path: '/search', query: { q: pageData.searchKey } })
}

const loadSuggestions = () => {
  window.clearTimeout(suggestionTimer)
  const prefix = pageData.searchKey.trim()
  if (!prefix) {
    suggestions.value = []
    return
  }
  suggestionTimer = window.setTimeout(async () => {
    const token = localStorage.getItem('token') || sessionStorage.getItem('token')
    if (!token) return
    try {
      const response = await fetch(`/api/search/keywords?prefix=${encodeURIComponent(prefix)}&limit=6`, {
        headers: { Authorization: `Bearer ${token}` }
      })
      const payload = await response.json()
      suggestions.value = payload.Status === '1' ? (payload.Data || []) : []
    } catch {
      suggestions.value = []
    }
  }, 180)
}

const chooseSuggestion = (keyword: string) => {
  pageData.searchKey = keyword
  suggestions.value = []
  getSearch()
}

onMounted(() => {
  pageData.searchKey = (route.query.q as string) || ''
})
</script>

<style lang="less" scoped>
/* 容器居中 */
.search-box {
  display: flex;
  justify-content: center;
  padding: 2rem 1rem;
}

.search-input-wrap {
  position: relative;
}

/* 默认：移动端 & 小屏 */
.search-input {
  width: 90vw;          /* 占 90% 视口宽 */
}

.suggestions-panel {
  position: absolute;
  top: calc(100% + 8px);
  left: 0;
  right: 0;
  z-index: 30;
  display: grid;
  gap: 4px;
  padding: 8px;
  background: #ffffff;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(29, 33, 41, 0.12);
}

.suggestion-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  padding: 8px 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: #1d2129;
  cursor: pointer;
  text-align: left;
}

.suggestion-item:hover {
  background: #f2f3f5;
}

.suggestion-item small {
  color: #86909c;
  white-space: nowrap;
}

/* ≥768 px：Pad / 普通 PC */
@media (min-width: 768px) {
  .search-input {
    width: 42rem;
  }
}

/* ≥1200 px：大屏 PC */
@media (min-width: 1200px) {
  .search-input {
    width: 52rem;       /* 适当拉长 */
  }
}
</style>
