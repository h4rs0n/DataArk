<template>
  <div class="index-view">
    <!-- 返回按钮 -->
    <div class="back-button-container">
      <a-button type="text" @click="goBack" class="back-button">
        <template #icon>
          <a-icon-arrow-left />
        </template>
        返回首页
      </a-button>
    </div>

    <div class="upload-container">
      <div class="header-section">
        <div class="icon-wrapper">
          <a-icon-upload class="header-icon" />
        </div>
        <h1 class="page-title">上传 SingleFile 保存的HTML文件</h1>
        <p class="page-subtitle">将您保存的网页文件上传到系统进行索引和管理</p>
      </div>

      <a-card class="upload-card" :bordered="false">
        <a-form :model="formData" @submit="handleSubmit" layout="vertical">
          <a-form-item
              field="domain"
              label="文件来源域名"
              validate-trigger="blur"
              :rules="[{ required: true, message: '请输入文件来源域名' }]"
              class="form-item-enhanced"
          >
            <a-input
                v-model="formData.domain"
                placeholder="请输入来源文件来源域名，例如: example.com"
                size="large"
                class="input-enhanced"
            >
              <template #prefix>
                <a-icon-link />
              </template>
            </a-input>
          </a-form-item>

          <a-form-item
              field="fileList"
              label="上传文件"
              :rules="[{ required: true, message: '请上传文件' }]"
              class="form-item-enhanced"
          >
            <a-upload
                draggable
                :action="uploadFileUrl"
                :limit="1"
                @success="handleSuccess"
                @error="handleError"
                @progress="handleProgress"
                accept=".html"
                :headers="authHeader"
                v-model:file-list="formData.fileList"
                class="upload-enhanced"
            >
              <template #upload-button>
                <div class="upload-demo">
                  <div class="upload-demo-icon">
                    <a-icon-upload class="upload-icon" />
                  </div>
                  <div class="upload-demo-text">
                    <p class="upload-main-text">点击或拖拽文件到此处上传</p>
                    <p class="upload-sub-text">仅支持 HTML 格式，单个文件最大 50MB</p>
                  </div>
                </div>
              </template>
            </a-upload>
          </a-form-item>

          <div class="form-actions">
            <a-button
                type="primary"
                html-type="submit"
                :loading="submitting"
                size="large"
                class="submit-button"
            >
              <template #icon>
                <a-icon-check />
              </template>
              {{ submitting ? '提交中...' : '提交' }}
            </a-button>
          </div>
        </a-form>
      </a-card>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, reactive, computed } from 'vue';
import { Notification } from '@arco-design/web-vue';
import { useRouter } from 'vue-router';

interface FormData {
  domain: string;
  fileList: any[];
}

const router = useRouter();
const uploadFileUrl = '/api/uploadHtmlFile';
const uploadUrl = '/api/upload';
const submitting = ref(false);
const uploading = ref(false);

const getAuthToken = () => {
  return localStorage.getItem('token') || sessionStorage.getItem('token')
}

const authHeader = computed<Record<string, string>>(() => {
  const headers: Record<string, string> = {}
  const token = getAuthToken()
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  return headers
})

const formData = reactive<FormData>({
  domain: '',
  fileList: []
});

// 检查是否有文件正在上传
const hasUploadingFile = computed(() => {
  return formData.fileList.some((file: any) => file.status === 'uploading');
});

const goBack = () => {
  router.push('/');
};

const handleSuccess = (file: any) => {
  uploading.value = false;
  Notification.success({
    title: '文件上传成功',
    content: '文件已成功上传，可以进行提交',
    position: 'topRight',
    duration: 3000,
  });
};

const redirectToLogin = () => {
  localStorage.removeItem('token')
  sessionStorage.removeItem('token')
  Notification.warning({
    title: '登录状态已过期',
    content: '请重新登录后再上传文件',
    position: 'topRight',
    duration: 4000,
  })
  router.push('/login')
}

const isUnauthorizedUploadError = (file: any) => {
  const statusCode = Number(
    file?.statusCode ?? file?.response?.statusCode ?? file?.response?.status ?? file?.xhr?.status ?? 0
  )
  if (statusCode === 401) {
    return true
  }

  const message = String(file?.response?.Message ?? file?.response?.message ?? '')
  return /valid token|unauthorized|authentication/i.test(message)
}

const handleError = (file: any) => {
  uploading.value = false;

  if (isUnauthorizedUploadError(file)) {
    redirectToLogin()
    return
  }

  Notification.error({
    title: '文件上传失败',
    content: '文件上传失败，请检查网络连接后重试',
    position: 'topRight',
    duration: 5000,
  });
};

const handleProgress = (file: any) => {
  uploading.value = true;
};

const handleSubmit = async () => {
  // 检查表单完整性
  if (formData.fileList.length === 0 || formData.domain === '') {
    Notification.error({
      title: '表单不完整',
      content: '请完成所有必填项后再提交',
      position: 'topRight',
      duration: 3000,
    });
    return;
  }

  // 检查是否有文件正在上传
  if (hasUploadingFile.value) {
    Notification.warning({
      title: '请等待上传完成',
      content: '文件正在上传中，请等待上传完成后再提交',
      position: 'topRight',
      duration: 4000,
    });
    return;
  }

  // 检查文件是否上传成功
  const hasFailedFile = formData.fileList.some((file: any) => file.status === 'error');
  if (hasFailedFile) {
    Notification.error({
      title: '文件上传失败',
      content: '存在上传失败的文件，请重新上传后再提交',
      position: 'topRight',
      duration: 4000,
    });
    return;
  }

  try {
    submitting.value = true;
    const submitData = {
      domain: formData.domain,
      files: formData.fileList
    };
    const token = getAuthToken()

    const response = await fetch(uploadUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {})
      },
      body: JSON.stringify(submitData)
    });

    if (!response.ok) {
      throw new Error('Upload failed');
    }

    Notification.success({
      title: '提交成功',
      content: '文件已成功索引，您可以在系统中查看和管理',
      position: 'topRight',
      duration: 4000,
    });

    // 清空表单
    formData.domain = '';
    formData.fileList = [];

  } catch (error) {
    Notification.error({
      title: '提交失败',
      content: '提交过程中发生错误，请检查网络连接后重试',
      position: 'topRight',
      duration: 5000,
    });
  } finally {
    submitting.value = false;
  }
};
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

.upload-container {
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

.upload-card {
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

.upload-enhanced {
  :deep(.arco-upload-draggable) {
    border: 1px dashed #94a3b8;
    border-radius: 8px;
    background: #f8fafc;
    transition: border-color 0.2s ease, background 0.2s ease;

    &:hover {
      border-color: #2563eb;
      background: #eff6ff;
    }
  }
}

.upload-demo {
  min-height: 176px;
  padding: 24px 20px;
  color: #334155;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
}

.upload-demo-icon {
  line-height: 1;
}

.upload-icon {
  font-size: 34px;
  color: #2563eb;
}

.upload-demo-text {
  .upload-main-text {
    font-size: 16px;
    font-weight: 600;
    color: #0f172a;
    margin: 0 0 8px;
  }

  .upload-sub-text {
    font-size: 14px;
    color: #64748b;
    margin: 0;
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

// 响应式设计
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

  .upload-container {
    padding-top: 24px;
  }

  .page-title {
    font-size: 28px;
  }

  .upload-card {
    padding: 18px;
  }

  .upload-demo {
    min-height: 156px;
    padding: 20px 16px;
  }

  .upload-main-text {
    font-size: 16px;
  }

  .submit-button {
    padding: 10px 32px;
    font-size: 14px;
  }
}

@media (max-width: 480px) {
  .upload-card {
    padding: 18px;
  }

  .form-item-enhanced {
    margin-bottom: 24px;
  }

  .upload-demo {
    padding: 20px 12px;
  }
}
</style>
