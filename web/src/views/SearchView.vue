<template>
  <div class="search-view">
    <div class="search-container">
      <!-- 搜索输入框 -->
      <div class="search-header">
        <search-input class="search-input"></search-input>
      </div>

      <!-- 结果信息 -->
      <div class="results-info" v-if="!errorStatus">
        <div class="results-meta">
          <span class="results-count">
            <icon-search class="search-icon" />
            约为 <strong>{{ TotalHits }}</strong> 条结果
          </span>
          <span class="search-term" v-if="pageData.searchKey">
            搜索："{{ pageData.searchKey }}"
          </span>
        </div>
      </div>

      <!-- 错误提示 -->
      <div class="error-area" v-if="errorStatus">
        <a-alert type="error" :message="errorMessage" show-icon>
          <template #icon>
            <icon-exclamation-circle />
          </template>
        </a-alert>
      </div>

      <!-- 搜索结果 -->
      <div class="results-container" v-if="!errorStatus">
        <transition-group name="fade-slide" tag="div" class="results-list">
          <div class="result-item" v-for="(item, index) in pageData.jsonResult.result" :key="item.filename" :style="{ animationDelay: `${index * 0.1}s` }">
            <a-card
                v-if="item.content != ''"
                class="result-card"
                :hoverable="true"
                :bordered="false"
            >
              <template #title>
                <div class="card-header">
                  <a-link
                      @click="htmlViewer(fileLink+item.domain+'/'+item.filename)"
                      target="_blank"
                      class="result-title"
                  >
                    {{ item.title }}
                  </a-link>
                  <div class="domain-badge">
                    <icon-globe />
                    {{ item.domain }}
                  </div>
                </div>
              </template>
              <template #extra>
                <a-link
                    :href="item.link"
                    target="_blank"
                    class="original-link"
                >
                  <icon-link />
                  <span class="link-text">原文链接</span>
                </a-link>
              </template>
              <div class="result-content-wrapper">
                <div class="result-content" v-html="item.content"></div>
                <div class="result-meta">
                  <span class="filename">
                    <icon-file />
                    {{ item.filename }}
                  </span>
                </div>
              </div>
            </a-card>
          </div>
        </transition-group>

        <!-- 空状态 -->
        <div class="empty-state" v-if="pageData.jsonResult.totalHits === 0">
          <div class="empty-icon">
            <icon-search />
          </div>
          <h3>未找到相关结果</h3>
          <p>尝试使用不同的关键词或检查拼写</p>
        </div>
      </div>

      <!-- 分页 -->
      <div class="pagination-container" v-if="!errorStatus && TotalHits != '0'">
        <a-pagination
            :total="Number(TotalHits)"
            :page-size="10"
            size="large"
            show-total
            show-jumper
            @change="changePage"
            :simple="isMobile"
        />
      </div>

      <!-- 返回顶部按钮 -->
      <a-back-top :style="{ right: '24px', bottom: '24px' }">
        <div class="back-to-top">
          <icon-up />
        </div>
      </a-back-top>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { useRoute, useRouter } from 'vue-router';
import { onMounted, reactive, watch, ref } from 'vue';

// 使用接口声明类型
interface ResultItem {
  title: string
  filename: string
  link: string
  content: string
  domain: string
}

let errorMessage = "搜索请求出现错误"
let errorStatus = ref(false)
let TotalHits = ref("0")
let pageNum = "1"
let fileLink = "/archive/"

// 响应式检测
const isMobile = ref(false)

// 双向绑定
let pageData = reactive({
  searchKey: "",
  jsonResult: {
    result: [] as ResultItem[],
    totalHits: 0
  },
});

const route = useRoute();
const router = useRouter();

const changePage = (currentPage: number) => {
  router.push({ path: '/search', query: { q: pageData.searchKey, p: currentPage } });
}

function queryData(keyword: string, pages : string = "1") {
  // let queryURL = `http://127.0.0.1:7845/api/search?q=${encodeURIComponent(keyword)}&p=${pages}`
  let queryURL = `/api/search?q=${encodeURIComponent(keyword)}&p=${pages}`
  const token = localStorage.getItem('token');

  fetch(queryURL, {
    method: 'GET',
    headers: (token ? { Authorization: `Bearer ${token}` } : {})
  })
      .then((response) => {
        if (!response.ok) {
          throw new Error('Network response was not ok');
        }
        return response.json();
      })
      .then((data) => {
        TotalHits.value = data.TotalHits

        if (data.Status == "0") {
          errorStatus.value = true
          errorMessage = data.Message
        }
        else {
          errorStatus.value = false
          pageData.jsonResult = { result: JSON.parse(data.Result), totalHits: JSON.parse(data.TotalHits) }
        }

      })
      .catch((error) => {
        errorStatus.value = true
        console.error('There was an error:', error);
      });
}

function htmlViewer(htmlLoc : string) {
  router.push({ path: '/htmlviewer', query: { loc: htmlLoc } })
}

// 检测移动设备
const checkMobile = () => {
  isMobile.value = window.innerWidth <= 768
}

onMounted(() => {
  pageData.searchKey = route.query.q! as string
  pageNum = route.query.p! as string

  checkMobile()
  window.addEventListener('resize', checkMobile)
});

const scrollToTop = () => {
  window.scrollTo({
    top: 0,
    left: 0,
    behavior: "smooth",
  });
};

// 监听q参数
watch(() => route.query.q, (newData) => {
  pageData.searchKey = newData! as string

  if(pageData.searchKey != null && pageData.searchKey != ""){
    queryData(pageData.searchKey)
  }
  else{
    errorMessage = "请输入关键字"
  }
}, { immediate: true });

// 监听p参数
watch(() => route.query.p, (newData) => {
  pageNum = newData! as string
  queryData(pageData.searchKey, pageNum)
  scrollToTop()
}, { immediate: true });

</script>

<style lang="less" scoped>
.search-view {
  min-height: 100vh;
  padding: 20px 0;

  @media (max-width: 768px) {
    padding: 10px 0;
  }
}

.search-container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 24px;

  @media (max-width: 768px) {
    padding: 0 16px;
  }
}

.search-header {
  margin-bottom: 32px;
  position: sticky;
  top: 0;
  z-index: 10;
  background: rgba(248, 250, 252, 0.9);
  backdrop-filter: blur(10px);
  padding: 16px 0;
  border-radius: 12px;

  @media (max-width: 768px) {
    margin-bottom: 24px;
    padding: 12px 0;
  }
}

.search-input {
  width: 100%;

  :deep(.arco-input-wrapper) {
    border-radius: 12px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
    border: 2px solid transparent;
    transition: all 0.3s ease;

    &:hover {
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
    }

    &.arco-input-focus {
      border-color: #1d39c4;
      box-shadow: 0 8px 24px rgba(29, 57, 196, 0.15);
    }
  }
}

.results-info {
  margin-bottom: 28px;

  .results-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;

    @media (max-width: 768px) {
      flex-direction: column;
      align-items: flex-start;
      gap: 8px;
    }
  }

  .results-count {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 16px;
    color: #4a5568;
    font-weight: 500;

    .search-icon {
      color: #1d39c4;
    }

    strong {
      color: #1d39c4;
    }

    @media (max-width: 768px) {
      font-size: 14px;
    }
  }

  .search-term {
    font-size: 14px;
    color: #718096;
    background: #edf2f7;
    padding: 4px 12px;
    border-radius: 20px;

    @media (max-width: 768px) {
      font-size: 12px;
      padding: 3px 10px;
    }
  }
}

.error-area {
  margin-bottom: 28px;

  :deep(.arco-alert) {
    border-radius: 12px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
    border: none;
  }
}

.results-container {
  margin-bottom: 48px;

  @media (max-width: 768px) {
    margin-bottom: 36px;
  }
}

.results-list {
  display: flex;
  flex-direction: column;
  gap: 24px;

  @media (max-width: 768px) {
    gap: 20px;
  }
}

.result-item {
  animation: fadeInUp 0.6s ease-out both;

  .result-card {
    border-radius: 16px;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    background: #ffffff;
    border: 1px solid #e2e8f0;
    overflow: hidden;

    &:hover {
      transform: translateY(-4px);
      box-shadow: 0 12px 40px rgba(0, 0, 0, 0.15);
      border-color: #1d39c4;
    }

    :deep(.arco-card-header) {
      padding: 24px 28px 20px;
      border-bottom: 1px solid #f1f5f9;
      background: linear-gradient(135deg, #ffffff 0%, #f8fafc 100%);

      @media (max-width: 768px) {
        padding: 20px 20px 16px;
      }
    }

    :deep(.arco-card-body) {
      padding: 24px 28px;

      @media (max-width: 768px) {
        padding: 20px 20px;
      }
    }

    .card-header {
      display: flex;
      align-items: flex-start;
      gap: 12px;
      flex-wrap: wrap;

      @media (max-width: 768px) {
        flex-direction: column;
        gap: 8px;
      }
    }

    .result-title {
      font-size: 20px;
      font-weight: 600;
      color: #1a202c;
      text-decoration: none;
      line-height: 1.4;
      flex: 1;
      min-width: 0;

      &:hover {
        color: #1d39c4;
        text-decoration: none;
      }

      @media (max-width: 768px) {
        font-size: 18px;
        line-height: 1.3;
      }
    }

    .domain-badge {
      display: flex;
      align-items: center;
      gap: 4px;
      background: #e6f3ff;
      color: #1d39c4;
      padding: 4px 12px;
      border-radius: 20px;
      font-size: 12px;
      font-weight: 500;
      white-space: nowrap;

      @media (max-width: 768px) {
        font-size: 11px;
        padding: 3px 10px;
      }
    }

    .original-link {
      display: flex;
      align-items: center;
      gap: 6px;
      font-size: 14px;
      color: #718096;
      text-decoration: none;
      padding: 8px 16px;
      border-radius: 8px;
      transition: all 0.2s ease;

      &:hover {
        color: #1d39c4;
        background: #f7fafc;
      }

      .link-text {
        @media (max-width: 480px) {
          display: none;
        }
      }

      @media (max-width: 768px) {
        font-size: 12px;
        padding: 6px 12px;
      }
    }

    .result-content-wrapper {
      .result-content {
        font-size: 15px;
        line-height: 1.7;
        color: #2d3748;
        margin-bottom: 16px;

        :deep(.highlight) {
          background: linear-gradient(135deg, #fed7aa 0%, #fef3c7 100%);
          color: #c05621;
          padding: 2px 6px;
          border-radius: 4px;
          font-weight: 600;
        }

        @media (max-width: 768px) {
          font-size: 14px;
          line-height: 1.6;
          margin-bottom: 12px;
        }
      }

      .result-meta {
        .filename {
          display: flex;
          align-items: center;
          gap: 6px;
          font-size: 12px;
          color: #a0aec0;
          background: #f7fafc;
          padding: 4px 12px;
          border-radius: 20px;
          width: fit-content;

          @media (max-width: 768px) {
            font-size: 11px;
            padding: 3px 10px;
          }
        }
      }
    }
  }
}

.empty-state {
  text-align: center;
  padding: 80px 20px;
  color: #718096;

  .empty-icon {
    font-size: 64px;
    margin-bottom: 24px;
    opacity: 0.5;

    @media (max-width: 768px) {
      font-size: 48px;
      margin-bottom: 16px;
    }
  }

  h3 {
    font-size: 24px;
    margin-bottom: 12px;
    color: #4a5568;

    @media (max-width: 768px) {
      font-size: 20px;
    }
  }

  p {
    font-size: 16px;

    @media (max-width: 768px) {
      font-size: 14px;
    }
  }
}

.pagination-container {
  display: flex;
  justify-content: center;
  padding: 32px 0;

  :deep(.arco-pagination) {
    .arco-pagination-item {
      border-radius: 8px;
      margin: 0 4px;
      border: 1px solid #e2e8f0;
      transition: all 0.2s ease;

      &:hover {
        border-color: #1d39c4;
        transform: translateY(-1px);
      }

      &.arco-pagination-item-active {
        border-color: #1d39c4;
        box-shadow: 0 4px 12px rgba(29, 57, 196, 0.3);
      }
    }

    .arco-pagination-item-jumper {
      .arco-input {
        border-radius: 8px;
        border: 1px solid #e2e8f0;
      }
    }
  }

  @media (max-width: 768px) {
    padding: 24px 0;

    :deep(.arco-pagination) {
      .arco-pagination-item {
        margin: 0 2px;
      }
    }
  }
}

.back-to-top {
  width: 48px;
  height: 48px;
  background: linear-gradient(135deg, #1d39c4 0%, #3b82f6 100%);
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 20px;
  box-shadow: 0 4px 20px rgba(29, 57, 196, 0.3);
  transition: all 0.3s ease;

  &:hover {
    transform: scale(1.1);
    box-shadow: 0 8px 30px rgba(29, 57, 196, 0.4);
  }

  @media (max-width: 768px) {
    width: 40px;
    height: 40px;
    font-size: 16px;
  }
}

// 动画
@keyframes fadeInUp {
  from {
    opacity: 0;
    transform: translateY(30px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.fade-slide-enter-active {
  transition: all 0.6s cubic-bezier(0.4, 0, 0.2, 1);
}

.fade-slide-leave-active {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.fade-slide-enter-from {
  opacity: 0;
  transform: translateY(30px);
}

.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-30px);
}

// 响应式断点
@media (max-width: 480px) {
  .search-view {
    padding: 8px 0;
  }

  .search-container {
    padding: 0 12px;
  }

  .result-item .result-card {
    border-radius: 12px;

    :deep(.arco-card-header) {
      padding: 16px 16px 12px;
    }

    :deep(.arco-card-body) {
      padding: 16px 16px;
    }

    .result-title {
      font-size: 16px;
    }

    .result-content-wrapper .result-content {
      font-size: 13px;
    }
  }
}

// 暗色模式支持
@media (prefers-color-scheme: dark) {
  .search-view {
    background: linear-gradient(135deg, #0f172a 0%, #1e293b 100%);
  }

  .search-header {
    background: rgba(15, 23, 42, 0.9);
  }

  .search-input {
    :deep(.arco-input-wrapper) {
      background: #1e293b;
      border-color: #334155;

      &.arco-input-focus {
        border-color: #4a9eff;
      }
    }
  }

  .results-info {
    .results-count {
      color: #cbd5e1;

      .search-icon {
        color: #4a9eff;
      }

      strong {
        color: #4a9eff;
      }
    }

    .search-term {
      background: #334155;
      color: #94a3b8;
    }
  }

  .result-item .result-card {
    background: #1e293b;
    border-color: #334155;

    &:hover {
      border-color: #4a9eff;
    }

    :deep(.arco-card-header) {
      background: linear-gradient(135deg, #1e293b 0%, #334155 100%);
      border-bottom-color: #334155;
    }

    .result-title {
      color: #f8fafc;

      &:hover {
        color: #4a9eff;
      }
    }

    .domain-badge {
      background: rgba(74, 158, 255, 0.2);
      color: #4a9eff;
    }

    .original-link {
      color: #94a3b8;

      &:hover {
        color: #4a9eff;
        background: #334155;
      }
    }

    .result-content-wrapper {
      .result-content {
        color: #cbd5e1;

        :deep(.highlight) {
          background: linear-gradient(135deg, #451a03 0%, #92400e 100%);
          color: #fbbf24;
        }
      }

      .result-meta .filename {
        background: #334155;
        color: #64748b;
      }
    }
  }

  .empty-state {
    color: #94a3b8;

    h3 {
      color: #cbd5e1;
    }
  }
}
</style>