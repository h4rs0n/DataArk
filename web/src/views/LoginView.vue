<script setup lang="ts">
import { ref } from 'vue';
import { Message } from '@arco-design/web-vue';
// 如果你的项目未全局注册组件/图标，请手动引入
import { IconUser, IconLock } from '@arco-design/web-vue/es/icon';
import axios from 'axios';
import { useRouter } from 'vue-router';

const formRef = ref();
const form = ref({
  username: '',
  password: '',
  remember: true,
});

const rules = {
  username: [{ required: true, message: '请输入用户名' }],
  password: [{ required: true, message: '请输入密码' }],
};

const router = useRouter();

function handleSubmit() {
  formRef.value.validate(async (errors: any) => {
    if (!errors) {
      try {
        const response = await fetch('/api/login', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            username: form.value.username,
            password: form.value.password,
          }),
        });

        const res = await response.json();

        if (res.Code === '1') {
          Message.success('登录成功');
          localStorage.setItem('token', res.Data.token);
          router.push('/');
        } else {
          Message.error(res.Message || '登录失败');
        }
      } catch (e) {
        Message.error('网络错误或服务器异常');
      }
    }
  });
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <h1 class="title">登录</h1>
      <!-- arco-design 表单 -->
      <a-form
          ref="formRef"
          :model="form"
          :rules="rules"
          layout="vertical"
          @submit="handleSubmit"
      >
        <!-- 用户名 -->
        <a-form-item field="username" label="用户名">
          <a-input v-model="form.username" placeholder="请输入用户名">
            <template #prefix>
              <icon-user />
            </template>
          </a-input>
        </a-form-item>

        <!-- 密码 -->
        <a-form-item field="password" label="密码">
          <a-input-password v-model="form.password" placeholder="请输入密码">
            <template #prefix>
              <icon-lock />
            </template>
          </a-input-password>
        </a-form-item>

        <!-- 记住我 & 忘记密码 -->
        <div class="actions">
          <!--
          <a-checkbox v-model="form.remember">记住我</a-checkbox>
          <a-link href="#" class="forgot">忘记密码?</a-link>
          -->
        </div>

        <!-- 登录按钮 -->
        <a-button type="primary" long class="login-btn" @click="handleSubmit">
          登录
        </a-button>
      </a-form>
    </div>
  </div>
</template>

<style scoped>
/* 页面整体背景：白 ➔ 淡蓝渐变 */
.login-page {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: linear-gradient(135deg, #ffffff 0%, #e6f1ff 100%);
  padding: 24px;
}

/* 登录卡片 */
.login-card {
  width: 100%;
  max-width: 420px;
  background: #fff;
  padding: 40px 32px 32px;
  border-radius: 12px;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.05);
}

.title {
  text-align: center;
  margin-bottom: 24px;
  color: #165dff; /* arco 默认主题蓝 */
}

.actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.login-btn {
  margin-top: 8px;
}

/* PC 端适配 (≥ 992px) */
@media (min-width: 992px) {
  .login-card {
    max-width: 460px;
    padding: 48px 40px 40px;
  }
}

/* 移动端适配 (≤ 480px) */
@media (max-width: 480px) {
  .login-card {
    padding: 24px 20px 20px;
  }

  .actions {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
}
</style>
