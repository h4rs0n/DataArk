<template>
  <div class="index-view">
    <p class="page-title">上传Singlefile保存的HTML文件</p>
    <a-card :style="{ width: '100%', maxWidth: '700px', padding: '30px' }">
      <a-form :model="formData" @submit="handleSubmit">
        <a-form-item field="domain" label="文件来源域名" validate-trigger="blur"
                     :rules="[{ required: true, message: '请输入文件来源域名' }]">
          <a-input v-model="formData.domain" placeholder="请输入来源文件来源域名，例如: example.com" />
        </a-form-item>

        <a-form-item field="fileList" label="上传文件"
                     :rules="[{ required: true, message: '请上传文件' }]">
          <a-upload
              draggable
              :action="uploadFileUrl"
              :limit="1"
              @success="handleSuccess"
              @error="handleError"
              accept=".html"
              :headers="authHeader"
              v-model:file-list="formData.fileList"
          >
            <template #upload-button>
              <div class="upload-demo">
                <div class="upload-demo-text">
                  <a-icon-upload />
                  <p>点击或拖拽文件到此处上传</p>
                  <p class="upload-demo-sub">仅支持 HTML 格式</p>
                </div>
              </div>
            </template>
          </a-upload>
        </a-form-item>

        <div class="form-actions">
          <a-button type="primary" html-type="submit" :loading="submitting">
            提交
          </a-button>
        </div>
      </a-form>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
import { ref, reactive } from 'vue';
import { Notification } from '@arco-design/web-vue';

interface FormData {
  domain: string;
  fileList: any[];
}

const uploadFileUrl = '/api/uploadHtmlFile';
const uploadUrl = '/api/upload';
const submitting = ref(false);
const token = localStorage.getItem('token');
const authHeader = { Authorization: `Bearer ${token}` }

const formData = reactive<FormData>({
  domain: '',
  fileList: []
});

const handleSuccess = (file: any) => {
  Notification.success({
    title: '提交成功',
    content: '文件已成功上传',
    position: 'topRight',
  });
};

const handleError = (file: any) => {
  Notification.error({
    title: '上传失败',
    content: '文件上传失败，请重试',
    position: 'topRight'
  });
};

const handleSubmit = async () => {
  if (formData.fileList.length === 0 || formData.domain === '') {
    Notification.error({
      title: '上传失败',
      content: '请完成所有必填项',
      position: 'topRight'
    });
    return;
  }


  try {
    submitting.value = true;
    const submitData = {
      domain: formData.domain,
      files: formData.fileList
    };

    await fetch(uploadUrl, {
      method: 'POST',
      headers: (token ? { Authorization: `Bearer ${token}` } : {}),
      body: JSON.stringify(submitData)
    });

    Notification.success({
      title: '提交成功',
      content: '文件已成功索引',
      position: 'topRight'
    });

  } catch (error) {
    Notification.error({
      title: '上传失败',
      content: '提交失败，请重试',
      position: 'topRight'
    });
  } finally {
    submitting.value = false;
  }
};
</script>

<style lang="less" scoped>
.index-view {
  display: grid;
  place-items: center;
  width: 100%;
  margin-top: 5vh;
  margin-bottom: 2vh;
}

.page-title {
  text-align: center;
  font-size: 20px;
  font-weight: 600;
  margin-bottom: 20px;
  color: #1890ff;
}

.upload-demo {
  padding: 20px;

  &-text {
    text-align: center;

    .icon-upload {
      font-size: 28px;
      color: rgb(var(--primary-6));
    }

    p {
      margin: 10px 0 0;
    }

    .upload-demo-sub {
      color: var(--color-text-3);
      font-size: 13px;
    }
  }
}

.error-area {
  margin-top: 16px;
}

.form-actions {
  margin-top: 24px;
  text-align: center;
}

@media (max-width: 768px) {
  .index-view {
    margin-top: 10vh;
    margin-bottom: 5vh;
    padding: 20px;
  }

  .a-card {
    width: 100%;
  }

  .page-title {
    font-size: 18px;
    margin-bottom: 15px;
  }

  .upload-demo-text p {
    font-size: 14px;
  }
}
</style>
