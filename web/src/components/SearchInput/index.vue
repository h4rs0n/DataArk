<template>
  <div class="search-box">
    <a-input-search
        size="large"
        v-model="pageData.searchKey"
        class="search-input"
        placeholder="输入搜索内容"
        @search="getSearch"
        @keydown.enter.native="getSearch"
        search-button
    />
  </div>
</template>

<script setup lang="ts">
import { Message } from '@arco-design/web-vue'
import { useRoute, useRouter } from 'vue-router'
import { onMounted, reactive } from 'vue'

const pageData = reactive({ searchKey: '' })

const route = useRoute()
const router = useRouter()

const getSearch = () => {
  if (pageData.searchKey === '' ){
    Message.warning('请输入需要搜索的内容')
  }
  if (!pageData.searchKey.trim()) return
  router.push({ path: '/search', query: { q: pageData.searchKey } })
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

/* 默认：移动端 & 小屏 */
.search-input {
  width: 90vw;          /* 占 90% 视口宽 */
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