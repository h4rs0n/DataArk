<template>
  <section class="panel settings-grid">
    <form class="settings-form" @submit.prevent="saveSettings">
      <h2>日报设置</h2>
      <label class="source-field" for="daily-limit">
        <span>每日数量</span>
        <input id="daily-limit" v-model.number="settingsForm.dailyLimit" name="daily-limit" type="number" min="1" max="50" />
      </label>
      <label class="source-field" for="candidate-window-days">
        <span>候选窗口天数</span>
        <input id="candidate-window-days" v-model.number="settingsForm.candidateWindowDays" name="candidate-window-days" type="number" min="1" max="365" />
      </label>
      <label class="source-field" for="recommendation-timezone">
        <span>时区</span>
        <input id="recommendation-timezone" v-model="settingsForm.timezone" name="recommendation-timezone" placeholder="Asia/Shanghai" />
      </label>
      <label class="source-field" for="generation-time">
        <span>生成时间</span>
        <input id="generation-time" v-model="settingsForm.generationTime" name="generation-time" type="time" />
      </label>
      <label class="source-field"><span>偏好主题（逗号分隔）</span><input v-model="preferredTopicsText" placeholder="Go, 数据库" /></label>
      <label class="source-field"><span>偏好语言（逗号分隔）</span><input v-model="preferredLanguagesText" placeholder="zh, en" /></label>
      <label class="source-field"><span>文章长度</span><select v-model="settingsForm.preferredLength"><option value="">不指定</option><option value="short">短文</option><option value="long">长文</option></select></label>
      <label class="source-field"><span>探索比例 {{ Math.round(settingsForm.explorationRate * 100) }}%</span><input v-model.number="settingsForm.explorationRate" type="range" min="0" max="1" step="0.05" /></label>
      <label class="source-field"><span>明确收藏来源（逗号分隔）</span><input v-model="favoriteSourcesText" placeholder="example.com" /></label>
      <label class="toggle-row">
        <input id="recommendation-enabled" v-model="settingsForm.enabled" name="recommendation-enabled" type="checkbox" />
        <span>启用日报生成</span>
      </label>
      <a-button type="primary" html-type="submit" :loading="savingSettings">
        <template #icon><icon-settings /></template>
        保存设置
      </a-button>
      <a-button status="danger" :loading="savingSettings" @click.prevent="resetPreferences">重置个人推荐偏好</a-button>
    </form>

    <section class="block-list">
      <h2>屏蔽规则</h2>
      <a-empty v-if="blockRules.length === 0" description="暂无屏蔽规则" />
      <article v-for="rule in blockRules" :key="rule.id" class="block-row">
        <span>{{ blockRuleTypeLabel(rule.type) }}</span>
        <strong>{{ rule.value }}</strong>
        <a-button size="small" status="danger" @click="deleteBlockRule(rule.id)">
          <template #icon><icon-delete /></template>
        </a-button>
      </article>
    </section>
  </section>
</template>

<script setup lang="ts">
// 推荐设置与屏蔽规则。
import { blockRuleTypeLabel, parseList, splitPreference } from '@/components/recommendations/format'
import { authHeaders, requestJSON } from '@/components/recommendations/http'
import type { BlockRule, RecommendationSettings } from '@/components/recommendations/types'
import { IconDelete, IconSettings } from '@arco-design/web-vue/es/icon'
import { Message } from '@arco-design/web-vue'
import { reactive, ref } from 'vue'

const savingSettings = ref(false)
const blockRules = ref<BlockRule[]>([])
const settingsForm = reactive<RecommendationSettings>({
  dailyLimit: 10,
  timezone: 'Asia/Shanghai',
  generationTime: '07:00',
  candidateWindowDays: 30,
  explorationRate: 0.15,
  preferredTopics: '[]',
  preferredLanguages: '[]',
  preferredLength: '',
  preferredDepth: 0,
  favoriteSources: '[]',
  enabled: false,
})
const preferredTopicsText = ref('')
const preferredLanguagesText = ref('')
const favoriteSourcesText = ref('')

async function loadSettings() {
  const settings = await requestJSON<RecommendationSettings>('/api/recommendations/settings', { headers: authHeaders() })
  Object.assign(settingsForm, settings)
  preferredTopicsText.value = parseList(settings.preferredTopics).join(', ')
  preferredLanguagesText.value = parseList(settings.preferredLanguages).join(', ')
  favoriteSourcesText.value = parseList(settings.favoriteSources).join(', ')
}

async function loadBlocks() {
  blockRules.value = (await requestJSON<BlockRule[]>('/api/recommendations/blocks', { headers: authHeaders() })) ?? []
}

async function saveSettings() {
  try {
    savingSettings.value = true
    const saved = await requestJSON<RecommendationSettings>('/api/recommendations/settings', {
      method: 'PUT',
      headers: authHeaders(true),
      body: JSON.stringify({
        ...settingsForm,
        preferredTopics: splitPreference(preferredTopicsText.value),
        preferredLanguages: splitPreference(preferredLanguagesText.value),
        favoriteSources: splitPreference(favoriteSourcesText.value),
      }),
    })
    Object.assign(settingsForm, saved)
    Message.success('推荐设置已保存')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '保存设置失败')
  } finally {
    savingSettings.value = false
  }
}

async function resetPreferences() {
  try {
    savingSettings.value = true
    await requestJSON('/api/recommendations/preferences/reset', { method: 'POST', headers: authHeaders() })
    await Promise.all([loadSettings(), loadBlocks()])
    Message.success('个人推荐偏好已重置，历史日报和反馈事件保留')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '重置失败')
  } finally {
    savingSettings.value = false
  }
}

async function deleteBlockRule(ruleId: number) {
  try {
    await requestJSON(`/api/recommendations/blocks/${ruleId}`, { method: 'DELETE', headers: authHeaders() })
    await loadBlocks()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除屏蔽规则失败')
  }
}

defineExpose({
  reload: () => Promise.all([loadSettings(), loadBlocks()]),
  loadBlocks,
})
</script>

<style scoped src="./panel-shared.css"></style>
<style scoped>
.settings-grid {
  display: grid;
  grid-template-columns: minmax(260px, 360px) minmax(0, 1fr);
  gap: 24px;
}

.settings-form,
.block-list {
  display: grid;
  align-content: start;
  gap: 14px;
}

.settings-form h2,
.block-list h2 {
  margin: 0;
  font-size: 20px;
}

.toggle-row,
.block-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.block-row {
  justify-content: space-between;
  padding: 12px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

@media (max-width: 820px) {
  .settings-grid {
    grid-template-columns: 1fr;
  }
}
</style>
