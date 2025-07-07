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
          <a-icon-cloud-upload class="header-icon" />
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
                    <a-icon-upload />
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
const token = localStorage.getItem('token');
const authHeader = { Authorization: `Bearer ${token}` }

const formData = reactive<FormData>({
  domain: '',
  fileList: []
});

// 检查是否有文件正在上传
const hasUploadingFile = computed(() => {
  return formData.fileList.some(file => file.status === 'uploading');
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

const handleError = (file: any) => {
  uploading.value = false;
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
  const hasFailedFile = formData.fileList.some(file => file.status === 'error');
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
  position: relative;
}

.back-button-container {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
}

.back-button {
  background: #e9f2ff;
  border: none;
  border-radius: 8px;
  padding: 8px 16px;
  font-weight: 500;
  color: #1890ff;
  transition: all 0.3s ease;

  &:hover {
    background: rgba(255, 255, 255, 1);
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  }
}

.upload-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  max-width: 800px;
  margin: 0 auto;
  padding-top: 60px;
}

.header-section {
  text-align: center;
  margin-bottom: 40px;
  color: #1890ff;
}

.icon-wrapper {
  margin-bottom: 20px;
}

.header-icon {
  font-size: 64px;
  color: rgba(255, 255, 255, 0.9);
  filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.2));
}

.page-title {
  font-size: 32px;
  font-weight: 700;
  margin: 0 0 12px 0;
  text-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
}

.page-subtitle {
  font-size: 16px;
  color: rgba(255, 255, 255, 0.8);
  margin: 0;
  line-height: 1.6;
}

.upload-card {
  width: 100%;
  max-width: 600px;
  border-radius: 16px;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.1);
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  padding: 40px;
}

.form-item-enhanced {
  margin-bottom: 32px;

  :deep(.arco-form-item-label) {
    font-weight: 600;
    color: #2c3e50;
    font-size: 16px;
    margin-bottom: 8px;
  }
}

.input-enhanced {
  border-radius: 12px;
  border: 2px solid #e1e8ed;
  transition: all 0.3s ease;

  &:hover {
    border-color: #1890ff;
  }

  &:focus-within {
    border-color: #1890ff;
    box-shadow: 0 0 0 3px rgba(24, 144, 255, 0.1);
  }

  :deep(.arco-input) {
    border: none;
    font-size: 16px;
  }

  :deep(.arco-input-prefix) {
    color: #8c9eff;
  }
}

.upload-enhanced {
  :deep(.arco-upload-draggable) {
    border: 2px dashed #d1d9ff;
    border-radius: 12px;
    background: #f8f9ff;
    transition: all 0.3s ease;

    &:hover {
      border-color: #1890ff;
      background: #f0f7ff;
    }
  }
}

.upload-demo {
  padding: 40px 20px;
  text-align: center;
}

.upload-demo-icon {
  margin-bottom: 20px;

  .icon-upload {
    font-size: 48px;
    color: #1890ff;
  }
}

.upload-demo-text {
  .upload-main-text {
    font-size: 18px;
    font-weight: 600;
    color: #2c3e50;
    margin: 0 0 8px 0;
  }

  .upload-sub-text {
    font-size: 14px;
    color: #8c9eff;
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
  border-radius: 12px;
  box-shadow: 0 4px 12px rgba(24, 144, 255, 0.3);
  transition: all 0.3s ease;

  &:hover {
    transform: translateY(-2px);
    box-shadow: 0 6px 20px rgba(24, 144, 255, 0.4);
  }
}

// 响应式设计
@media (max-width: 768px) {
  .index-view {
    padding: 10px;
  }

  .back-button-container {
    top: 10px;
    right: 10px;
  }

  .upload-container {
    padding-top: 50px;
  }

  .header-icon {
    font-size: 48px;
  }

  .page-title {
    font-size: 24px;
  }

  .page-subtitle {
    font-size: 14px;
  }

  .upload-card {
    padding: 24px;
    margin: 0 4px;
  }

  .upload-demo {
    padding: 24px 16px;
  }

  .upload-demo-icon .icon-upload {
    font-size: 36px;
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
  .page-title {
    font-size: 20px;
  }

  .upload-card {
    padding: 20px;
  }

  .form-item-enhanced {
    margin-bottom: 24px;
  }

  .upload-demo {
    padding: 20px 12px;
  }
}
</style>