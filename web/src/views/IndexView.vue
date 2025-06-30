<template>
  <div class="index-view">
    <!-- 右上角按钮容器 -->
    <div class="top-right-buttons">
      <a-button type="primary" @click="goToUpload" class="action-button">
        <template #icon>
          <icon-upload />
        </template>
        上传文件
      </a-button>
      <a-button status="danger" @click="logout" class="action-button">
        <template #icon>
          <icon-export />
        </template>
        退出登录
      </a-button>
    </div>
    <!--
    <div class="index-image-container">
      <img src="/archive.webp">
    </div>
    -->
    <search-input></search-input>
  </div>
</template>

<script lang="ts" setup>
import { useRouter } from 'vue-router';
// 引入 Arco Design 图标
import { IconUpload, IconExport } from '@arco-design/web-vue/es/icon';

const router = useRouter();

// 上传文件功能
const goToUpload = () => {
  router.push('/upload'); // 跳转到上传页面
};

// 退出登录功能
const logout = () => {
  // 实际项目中这里会有退出逻辑（清除token等）
  // 然后跳转到登录页
  router.push('/login');
};

// 保留原有的搜索功能
const search = () => {
  router.push({ path: '/search', query: { q: "searchInput.value" } });
}
</script>

<style lang="less" scoped>
.index-view {
  display: grid;
  place-items: center;
  width: 100vw;
  height: 100vh;
  position: relative; /* 为绝对定位按钮提供参考 */
}

.top-right-buttons {
  position: absolute;
  top: 20px;
  right: 20px;
  display: flex;
  gap: 12px;
  z-index: 10; /* 确保按钮在顶层 */
}

.action-button {
  /* 添加轻微阴影提升层次感 */
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  /* 平滑的过渡效果 */
  transition: all 0.2s ease;

  &:hover {
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  }

  &:active {
    transform: translateY(0);
  }
}

.index-image-container{
  width: 40vw;
  height: 50vh;
  img{
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
}

</style>