<template>
  <div class="recommendations-view">
    <div class="top-bar">
      <a-button type="text" class="back-button" @click="router.push('/')">
        <template #icon><icon-arrow-left /></template>
        返回首页
      </a-button>
    </div>

    <main class="content">
      <header class="page-header">
        <div class="header-mark"><icon-robot /></div>
        <div>
          <h1>推荐中心</h1>
          <p>今日推荐、历史日报、内容发现和推荐偏好统一管理。</p>
        </div>
        <a-button :loading="loading" @click="loadAll">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </header>

      <a-tabs v-model:active-key="activeTab" class="tabs">
        <a-tab-pane key="today" title="今日推荐">
          <section class="panel">
            <div class="section-title">
              <div>
                <h2>{{ todaySnapshot.day.date }}</h2>
                <span>{{ todayStatusText }}</span>
              </div>
              <a-space wrap>
                <a-button v-if="isOwner && todaySnapshot.day.actualCount < todaySnapshot.day.requestedCount && ['published', 'supplemented'].includes(todaySnapshot.day.status)" :loading="generating" type="primary" @click="supplementDaily(todaySnapshot.day.date)">
                  补充缺少文章
                </a-button>
                <a-button @click="loadToday">
                  <template #icon><icon-refresh /></template>
                  刷新
                </a-button>
              </a-space>
            </div>

            <DigestSummary :day="todaySnapshot.day" />

            <a-empty v-if="todaySnapshot.items.length === 0" description="暂无今日推荐" />
            <div v-else class="recommendation-list">
              <RecommendationArticleCard
                v-for="item in todaySnapshot.items"
                :key="item.id"
                :item="item"
                :archiving="archivingCandidateId === item.candidateId"
                :current-action="feedbackByItem[item.id]?.action"
                :feedback-loading="feedbackLoadingId === item.id"
                @open="openCandidate"
                @archive="archiveCandidate"
                @context="openItemContext"
                @mark-read="markCandidateRead"
                @feedback="(action) => sendFeedback(item, action)"
                @scope="(scope) => openImpactDialog(item, scope)"
                @revert="revertFeedback(item)"
              />
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="history" title="历史日报">
          <section class="panel">
            <div class="section-title">
              <div>
                <h2>历史日报</h2>
                <span>历史快照保持生成时的排序和理由</span>
              </div>
              <label class="inline-field" for="history-date">
                <span>日期</span>
                <input id="history-date" v-model="historyDate" type="date" @change="loadHistoryDay" />
              </label>
            </div>
            <div class="history-layout">
              <aside class="day-list">
                <button v-for="day in historyDays" :key="day.id" type="button" :class="{ active: day.date === historyDate }" @click="selectHistoryDay(day.date)">
                  <strong>{{ day.date }}</strong>
                  <span>{{ day.actualCount }}/{{ day.requestedCount }} · {{ day.status }}</span>
                </button>
              </aside>
              <div class="history-detail">
                <a-empty v-if="historySnapshot.items.length === 0" description="暂无历史推荐" />
                <template v-else>
                  <article v-for="item in historySnapshot.items" :key="item.id" class="compact-card">
                    <div class="item-main">
                      <div class="item-meta">
                        <span>#{{ item.rank }}</span>
                        <span>{{ item.candidate.sourceName || sourceHost(item.candidate.url) }}</span>
                      </div>
                      <h3>{{ item.candidate.title }}</h3>
                      <p>{{ item.candidate.summary || item.candidate.url }}</p>
                      <small>{{ item.reason }}</small>
                    </div>
                    <a-button @click="openCandidate(item.candidate)">
                      <template #icon><icon-link /></template>
                      原文
                    </a-button>
                  </article>
                </template>
              </div>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="discovery" title="内容发现">
          <section class="panel source-panel">
            <div class="section-title">
              <div>
                <h2>订阅源</h2>
                <span>仅显示管理员主动设置的第一优先级来源；友情链接博客在后台作为第二优先级扩展</span>
              </div>
            </div>
            <form v-if="isOwner" class="source-form" @submit.prevent="saveSource">
              <label class="source-field" for="source-name">
                <span>名称</span>
                <input id="source-name" v-model="sourceForm.name" name="source-name" placeholder="名称" />
              </label>
              <label class="source-field source-url" for="source-url">
                <span>URL</span>
                <input id="source-url" v-model="sourceForm.url" name="source-url" placeholder="https://example.com/feed.xml" />
              </label>
              <label class="source-field" for="source-type">
                <span>类型</span>
                <select id="source-type" v-model="sourceForm.type" name="source-type">
                  <option value="feed">RSS</option>
                  <option value="rsshub">RSSHub</option>
                  <option value="site">站点</option>
                </select>
              </label>
              <a-button type="primary" html-type="submit" :loading="savingSource">
                <template #icon><icon-plus /></template>
                添加
              </a-button>
            </form>

            <div class="source-list">
              <article v-for="source in sources" :key="source.id" class="source-row">
                <div class="item-main">
                  <h3>{{ source.name }}</h3>
                  <p>{{ source.url }}</p>
                  <span>第一优先级 · {{ source.type }} · {{ source.enabled ? '启用' : '停用' }}</span>
                  <small v-if="source.lastError">{{ source.lastError }}</small>
                </div>
                <a-space>
                  <a-button v-if="source.siteId" size="small" @click="loadSiteInsight(source)">图谱与回溯</a-button>
                  <a-button v-if="isOwner" size="small" :loading="fetchingSourceId === source.id" @click="fetchSource(source.id)">
                    <template #icon><icon-sync /></template>
                    获取
                  </a-button>
                  <a-button v-if="isOwner" size="small" status="danger" @click="deleteSource(source.id)">
                    <template #icon><icon-delete /></template>
                  </a-button>
                </a-space>
              </article>
            </div>
            <SiteInsightPanel :source="selectedSource" :graph="siteGraph" :operations="siteOperations" :backfills="siteBackfills" :is-owner="isOwner" @reload="reloadSiteInsight" @backfill="requestBackfill" @sitemap-backfill="requestSitemapBackfill" />
          </section>

          <section class="panel">
            <div class="section-title">
              <div>
                <h2>{{ candidateStatus === 'new' ? '猜你喜欢' : '候选文章' }}</h2>
                <span>{{ candidateStatus === 'new' ? '根据阅读与反馈偏好，每次推荐最多 10 篇符合条件的文章' : '查看已经阅读、忽略或加入归档队列的候选文章' }}</span>
              </div>
              <label class="inline-field" for="candidate-status">
                <span>状态</span>
                <select id="candidate-status" v-model="candidateStatus" name="candidate-status" @change="loadCandidates">
                  <option value="new">待读</option>
                  <option value="read">已读</option>
                  <option value="ignored">已忽略</option>
                  <option value="archived">已入库</option>
                </select>
              </label>
            </div>
            <a-spin v-if="candidateStatus === 'new'" :loading="discoveryFeedLoading" class="discovery-feed-spin">
              <a-empty v-if="discoveryFeed.items.length === 0" description="暂无符合推荐条件的文章" />
              <div v-else class="recommendation-list">
                <RecommendationArticleCard
                  v-for="item in discoveryFeed.items"
                  :key="item.id"
                  :item="item"
                  :archiving="archivingCandidateId === item.candidateId"
                  :current-action="feedbackByItem[item.id]?.action"
                  :feedback-loading="feedbackLoadingId === item.id"
                  @open="openCandidate"
                  @archive="archiveCandidate"
                  @context="openItemContext"
                  @mark-read="markCandidateRead"
                  @feedback="(action) => sendFeedback(item, action)"
                  @scope="(scope) => openImpactDialog(item, scope)"
                  @revert="revertFeedback(item)"
                />
              </div>
              <p v-if="discoveryFeed.batch && discoveryFeed.batch.actualCount < discoveryFeed.batch.requestedCount" class="feed-shortage">
                当前仅有 {{ discoveryFeed.batch.actualCount }} 篇符合推荐条件
              </p>
              <div class="feed-refresh-row">
                <a-button type="primary" :loading="refreshingDiscoveryFeed" @click="refreshDiscoveryFeedBatch">
                  <template #icon><icon-refresh /></template>
                  换一换
                </a-button>
              </div>
            </a-spin>
            <a-empty v-else-if="candidates.length === 0" description="暂无候选文章" />
            <div v-else class="item-list">
              <article v-for="candidate in candidates" :key="candidate.id" class="compact-card">
                <div class="item-main">
                  <h3>
                    <a
                      class="candidate-title-link"
                      :href="candidate.url"
                      target="_blank"
                      rel="noopener noreferrer"
                      @click="markCandidateRead(candidate)"
                    >
                      {{ candidate.title || candidate.url }}
                    </a>
                  </h3>
                  <p>{{ candidate.summary || candidate.url }}</p>
                  <span>{{ candidate.sourceName }} · 处理 {{ candidate.processingState || 'unknown' }} · 资格 {{ candidate.eligibilityState || 'unknown' }} · 去重 {{ candidate.dedupeState || 'unknown' }}</span>
                  <small>评估 {{ candidate.assessmentState || 'pending' }} · 正文 v{{ candidate.contentVersion || 0 }} · {{ candidate.userState?.currentFeedback || '无个人反馈' }}</small>
                </div>
                <a-space class="candidate-actions" wrap>
                  <a-button @click="openCandidate(candidate)">
                    <template #icon><icon-link /></template>
                    原文
                  </a-button>
                  <a-button type="primary" :loading="archivingCandidateId === candidate.id" @click="archiveCandidate(candidate.id)">
                    <template #icon><icon-storage /></template>
                    入库
                  </a-button>
                  <a-button status="danger" @click="ignoreCandidate(candidate.id)">
                    <template #icon><icon-close /></template>
                    忽略
                  </a-button>
                </a-space>
              </article>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane v-if="isOwner" key="queue" title="任务队列">
          <section class="panel">
            <div class="section-title">
              <div>
                <h2>爬取任务队列</h2>
                <span>自动发现只登记任务；点击后单次执行当前到期任务及其派生任务</span>
              </div>
              <a-space wrap>
                <span class="queue-state" :class="`queue-state-${crawlQueue.state}`">{{ crawlQueueStateLabel }}</span>
                <a-button :loading="crawlQueueLoading || blacklistLoading" @click="refreshQueueTab">
                  <template #icon><icon-refresh /></template>
                  刷新
                </a-button>
                <a-button type="primary" :loading="runningCrawlQueue" :disabled="!crawlQueue.canRun || crawlQueue.state === 'running'" @click="runCrawlQueue">
                  执行待处理任务
                </a-button>
              </a-space>
            </div>

            <div class="queue-summary">
              <div><strong>{{ crawlQueue.counts.pending }}</strong><span>等待执行</span></div>
              <div><strong>{{ crawlQueue.counts.running }}</strong><span>正在运行</span></div>
              <div><strong>{{ crawlQueue.counts.succeeded24h }}</strong><span>近 24 小时成功</span></div>
              <div><strong>{{ crawlQueue.counts.failed24h }}</strong><span>近 24 小时失败</span></div>
            </div>

            <a-empty v-if="displayedCrawlQueueTasks.length === 0" description="暂无爬取任务" />
            <div v-else class="queue-task-list">
              <template v-for="item in displayedCrawlQueueTasks" :key="item.key">
                <article v-if="item.type === 'task'" class="queue-task-row">
                  <div>
                    <strong>{{ crawlTaskKindLabel(item.task.kind) }}</strong>
                    <span>{{ crawlTaskTargetLabel(item.task) }}</span>
                  </div>
                  <span class="task-status" :class="`task-status-${item.task.status}`">{{ crawlTaskStatusLabel(item.task.status) }}</span>
                  <span>尝试 {{ item.task.attempts }} 次</span>
                  <span>{{ crawlTaskTimeLabel(item.task) }}</span>
                  <small v-if="item.task.error" class="queue-task-error">{{ item.task.error }}</small>
                </article>
                <article v-else class="queue-task-row queue-task-summary">
                  <div>
                    <strong>{{ crawlTaskKindLabel(item.kind) }}</strong>
                    <span>另有 {{ item.collapsedCount }} 条相同任务已合并<span v-if="item.contentVersion"> · 正文 v{{ item.contentVersion }}</span></span>
                  </div>
                  <span class="task-status" :class="`task-status-${item.status}`">{{ crawlTaskStatusLabel(item.status) }}</span>
                  <span>尝试 {{ item.attempts }} 次</span>
                  <span>{{ crawlTaskGroupTimeLabel(item) }}</span>
                  <small v-if="item.error" class="queue-task-error">{{ item.error }}</small>
                </article>
              </template>
            </div>
          </section>

          <section class="panel domain-blacklist-panel">
            <div class="section-title">
              <div>
                <h2>域名黑名单</h2>
                <span>命中域名及其子域不会再被访问，已爬取文章也会从候选列表和后续推荐中剔除</span>
              </div>
            </div>
            <form class="blacklist-form" @submit.prevent="addDomainBlacklist">
              <label class="source-field" for="blacklist-domain">
                <span>域名</span>
                <input id="blacklist-domain" v-model="blacklistForm.domain" name="blacklist-domain" autocomplete="off" placeholder="example.com" />
              </label>
              <label class="source-field" for="blacklist-reason">
                <span>备注（可选）</span>
                <input id="blacklist-reason" v-model="blacklistForm.reason" name="blacklist-reason" autocomplete="off" placeholder="屏蔽原因" />
              </label>
              <a-button type="primary" html-type="submit" :loading="addingBlacklist">添加域名</a-button>
            </form>
            <a-spin :loading="blacklistLoading">
              <a-empty v-if="domainBlacklist.length === 0" description="暂无域名黑名单" />
              <div v-else class="blacklist-list">
                <article v-for="entry in domainBlacklist" :key="entry.id" class="blacklist-row">
                  <div><strong>{{ entry.domain }}</strong><span>{{ entry.reason || '未填写备注' }}</span></div>
                  <span>同时匹配所有子域 · {{ formatDateTime(entry.createdAt) }}</span>
                  <a-popconfirm content="删除后，不再被其他规则覆盖的候选文章将恢复显示，未完成任务也会恢复待抓取。确认删除？" @ok="deleteDomainBlacklist(entry.id)">
                    <a-button size="small" status="danger" :loading="deletingBlacklistId === entry.id">
                      <template #icon><icon-delete /></template>
                      删除
                    </a-button>
                  </a-popconfirm>
                </article>
              </div>
            </a-spin>
          </section>
        </a-tab-pane>

        <a-tab-pane key="settings" title="推荐设置">
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
        </a-tab-pane>

        <a-tab-pane key="ranking" title="点击排行">
          <section class="panel">
            <div class="section-title">
              <h2>被点击网页文件排行榜</h2>
              <a-radio-group v-model="rankingWindow" type="button" size="small" @change="loadRankings">
                <a-radio value="7d">7 天</a-radio>
                <a-radio value="all">总榜</a-radio>
              </a-radio-group>
            </div>
            <a-empty v-if="rankings.length === 0" description="暂无点击记录" />
            <div v-else class="item-list">
              <article v-for="item in rankings" :key="item.path" class="compact-card">
                <div class="item-main">
                  <h3>{{ item.title || item.fileName }}</h3>
                  <p>{{ item.summary || item.path }}</p>
                  <span>{{ item.domain }} · {{ item.clickCount }} 次点击</span>
                </div>
                <a-button type="primary" @click="openArchive(item.path)">
                  <template #icon><icon-eye /></template>
                  查看
                </a-button>
              </article>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="keywords" title="搜索热词">
          <section class="panel">
            <div class="section-title">
              <h2>最常用搜索关键词</h2>
              <a-radio-group v-model="keywordWindow" type="button" size="small" @change="loadKeywords">
                <a-radio value="7d">7 天</a-radio>
                <a-radio value="all">总榜</a-radio>
              </a-radio-group>
            </div>
            <a-empty v-if="keywords.length === 0" description="暂无搜索记录" />
            <div v-else class="keyword-grid">
              <button v-for="item in keywords" :key="item.keyword" class="keyword-tile" type="button" @click="searchKeyword(item.keyword)">
                <strong>{{ item.keyword }}</strong>
                <span>{{ item.count }} 次 · {{ item.resultCount }} 条结果</span>
              </button>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="archives" title="归档推荐">
          <section class="panel">
            <div class="section-title">
              <h2>网页归档文件推荐</h2>
              <span>基于点击、搜索词和内容元数据</span>
            </div>
            <a-empty v-if="archiveRecommendations.length === 0" description="暂无可推荐归档" />
            <div v-else class="item-list">
              <article v-for="item in archiveRecommendations" :key="item.path" class="compact-card">
                <div class="item-main">
                  <h3>{{ item.title || item.fileName }}</h3>
                  <p>{{ item.summary || item.path }}</p>
                  <span>{{ item.domain }} · {{ item.reason }} · 分数 {{ item.score.toFixed(1) }}</span>
                </div>
                <a-button type="primary" @click="openArchive(item.path)">
                  <template #icon><icon-eye /></template>
                  查看
                </a-button>
              </article>
            </div>
          </section>
        </a-tab-pane>
      </a-tabs>
    </main>

    <a-modal v-model:visible="impactDialog.visible" :title="impactDialogTitle" :ok-loading="feedbackLoadingId === impactDialog.item?.id" @ok="submitImpactFeedback">
      <div class="block-options">
        <label v-for="option in impactOptions" :key="`${option.type}:${option.value}`">
          <input v-model="selectedImpactValue" type="radio" :value="option.value" />
          <span>{{ blockRuleTypeLabel(option.type) }}：{{ option.value }}</span>
        </label>
      </div>
    </a-modal>

    <a-drawer v-model:visible="contextDrawerVisible" :width="520" title="推荐追溯与文章评估">
      <a-spin :loading="contextLoading" style="width:100%">
        <template v-if="itemContext">
          <h3>{{ itemContext.item.snapshotTitle }}</h3>
          <p>文章评估：{{ itemContext.assessment ? `${Math.round(itemContext.assessment.overallQuality * 100)} 分 · ${itemContext.assessment.assessor}` : '规则评估详情不可用' }}</p>
          <p>个人状态：{{ itemContext.userState?.currentFeedback || '无反馈' }}</p>
          <article v-for="entry in itemContext.provenance" :key="entry.provenance.id" class="trace-entry">
            <strong>{{ entry.site.displayName || entry.site.hostKey }}</strong>
            <span>{{ entry.provenance.discoveryMethod }} · {{ entry.provenance.sourcePageUrl || entry.provenance.originalUrl }}</span>
            <span>路径：{{ (entry.graph?.shortestSeedPath?.sites || []).map((site:any) => site.displayName || site.hostKey).join(' → ') || '种子站点' }}</span>
          </article>
        </template>
      </a-spin>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Message, Notification } from '@arco-design/web-vue'
import DigestSummary from '@/components/recommendations/DigestSummary.vue'
import RecommendationArticleCard from '@/components/recommendations/RecommendationArticleCard.vue'
import SiteInsightPanel from '@/components/recommendations/SiteInsightPanel.vue'
import { groupCrawlQueueTasks } from '@/utils/crawlQueueGrouping.mjs'
import {
  IconArrowLeft,
  IconClose,
  IconDelete,
  IconEye,
  IconLink,
  IconPlus,
  IconRefresh,
  IconRobot,
  IconSettings,
  IconStorage,
  IconSync,
} from '@arco-design/web-vue/es/icon'

type FeedbackAction = 'valuable' | 'not_interested' | 'too_repetitive' | 'low_value' | 'deep_read' | 'block_source' | 'reduce_topic' | 'reduce_style'
type BlockRuleType = 'topic' | 'source' | 'style'

interface ArchiveRankingItem {
  path: string
  domain: string
  fileName: string
  title: string
  summary: string
  clickCount: number
}

interface ArchiveRecommendationItem extends ArchiveRankingItem {
  score: number
  reason: string
}

interface KeywordItem {
  keyword: string
  count: number
  resultCount: number
}

interface DiscoverySource {
  id: number
  name: string
  url: string
  type: string
  enabled: boolean
  lastError: string
  siteId?: number
  endpointType?: string
  nextDueAt?: string
  nextFetchAt?: string
  lastSuccessAt?: string
  userManaged?: boolean
}

interface DiscoveryCandidate {
  id: number
  sourceName: string
  url: string
  title: string
  summary: string
  status: string
  score: number
  topics?: string
  contentType?: string
  contentStyle?: string
  enrichmentStatus?: string
  publishedAt?: string
  processingState?: string
  eligibilityState?: string
  dedupeState?: string
  assessmentState?: string
  contentVersion?: number
  userState?: { currentFeedback?: string; openedAt?: string; archivedAt?: string }
}

interface RecommendationDay {
  id: number
  userId: number
  date: string
  timezone: string
  status: string
  requestedCount: number
  actualCount: number
  generatedAt?: string
  shortageReasons?: string
  degraded?: boolean
  degradationReason?: string
}

interface RecommendationItem {
  id: number
  candidateId: number
  rank: number
  reason: string
  rerankScore: number
  poolType?: string
  explorationReason?: string
  snapshotTitle?: string
  candidate: DiscoveryCandidate
}

interface RecommendationSnapshot {
  day: RecommendationDay
  items: RecommendationItem[]
}

interface RecommendationFeedBatch {
  id: number
  userId: number
  status: string
  requestedCount: number
  actualCount: number
  policyVersion: string
  profileVersion: number
  shortageReasons: string
  createdAt: string
}

interface RecommendationFeedSnapshot {
  batch: RecommendationFeedBatch | null
  items: RecommendationItem[]
}

interface RecommendationSettings {
  dailyLimit: number
  timezone: string
  generationTime: string
  candidateWindowDays: number
  explorationRate: number
  preferredTopics: string
  preferredLanguages: string
  preferredLength: string
  preferredDepth: number
  favoriteSources: string
  enabled: boolean
}

interface BlockRule {
  id: number
  type: BlockRuleType
  value: string
  active: boolean
}

interface BlockTarget {
  type: BlockRuleType
  value: string
}

type CrawlTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

interface CrawlQueueTask {
  id: string
  kind: string
  targetType: string
  targetId: number
  contentVersion?: string
  status: CrawlTaskStatus
  attempts: number
  scheduledAt?: string
  startedAt?: string
  finishedAt?: string
  error?: string
  createdAt: string
}

interface CrawlQueueSnapshot {
  mode: 'manual'
  state: 'idle' | 'waiting' | 'running'
  canRun: boolean
  updatedAt: string
  counts: { pending: number; running: number; succeeded24h: number; failed24h: number }
  tasks: CrawlQueueTask[]
}

interface DomainBlacklistEntry {
  id: number
  domain: string
  reason: string
  createdAt: string
  updatedAt: string
}

interface DomainBlacklistMutation {
  entry: DomainBlacklistEntry
  affectedCandidates: number
}

const emptyCrawlQueue = (): CrawlQueueSnapshot => ({
  mode: 'manual',
  state: 'idle',
  canRun: false,
  updatedAt: '',
  counts: { pending: 0, running: 0, succeeded24h: 0, failed24h: 0 },
  tasks: [],
})

const router = useRouter()
const activeTab = ref('today')
const loading = ref(false)
const generating = ref(false)
const savingSettings = ref(false)
const savingSource = ref(false)
const fetchingSourceId = ref<number | null>(null)
const archivingCandidateId = ref<number | null>(null)
const feedbackLoadingId = ref<number | null>(null)
const discoveryFeedLoading = ref(false)
const refreshingDiscoveryFeed = ref(false)
const crawlQueueLoading = ref(false)
const runningCrawlQueue = ref(false)
const blacklistLoading = ref(false)
const addingBlacklist = ref(false)
const deletingBlacklistId = ref<number | null>(null)
const rankingWindow = ref<'7d' | 'all'>('7d')
const keywordWindow = ref<'7d' | 'all'>('7d')
const candidateStatus = ref('new')
const historyDate = ref(formatLocalDate(new Date()))
const rankings = ref<ArchiveRankingItem[]>([])
const archiveRecommendations = ref<ArchiveRecommendationItem[]>([])
const keywords = ref<KeywordItem[]>([])
const sources = ref<DiscoverySource[]>([])
const candidates = ref<DiscoveryCandidate[]>([])
const discoveryFeed = ref<RecommendationFeedSnapshot>({ batch: null, items: [] })
const crawlQueue = ref<CrawlQueueSnapshot>(emptyCrawlQueue())
const domainBlacklist = ref<DomainBlacklistEntry[]>([])
const historyDays = ref<RecommendationDay[]>([])
const blockRules = ref<BlockRule[]>([])
const feedbackByItem = reactive<Record<number, { action: string } | undefined>>({})
const isOwner = ref(false)
const selectedSource = ref<DiscoverySource>()
const siteGraph = ref<any>()
const siteOperations = ref<any>()
const siteBackfills = ref<any[]>([])
const contextDrawerVisible = ref(false)
const contextLoading = ref(false)
const itemContext = ref<any>()
const todaySnapshot = ref<RecommendationSnapshot>(emptySnapshot(formatLocalDate(new Date())))
const historySnapshot = ref<RecommendationSnapshot>(emptySnapshot(historyDate.value))
const sourceForm = reactive({ name: '', url: '', type: 'feed' })
const blacklistForm = reactive({ domain: '', reason: '' })
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
const impactDialog = reactive<{ visible: boolean; item: RecommendationItem | null; scope: BlockRuleType }>({ visible: false, item: null, scope: 'source' })
const selectedImpactValue = ref('')
let crawlQueueTimer: ReturnType<typeof window.setTimeout> | null = null

const crawlQueueStateLabel = computed(() => ({
  idle: '队列空闲',
  waiting: '等待手动执行',
  running: '正在执行',
}[crawlQueue.value.state]))

const displayedCrawlQueueTasks = computed(() => groupCrawlQueueTasks(crawlQueue.value.tasks))

const todayStatusText = computed(() => {
  const day = todaySnapshot.value.day
  if (day.status === 'published' || day.status === 'supplemented') {
    return `${day.actualCount}/${day.requestedCount} 篇 · ${day.generatedAt ? formatDateTime(day.generatedAt) : '已生成'}`
  }
  if (day.status === 'missing') return '尚未生成'
  return day.status
})

const impactOptions = computed<BlockTarget[]>(() => {
  const item = impactDialog.item
  if (!item) return []
  const candidate = item.candidate
  const options: BlockTarget[] = []
  if (impactDialog.scope === 'topic') {
    for (const topic of parseList(candidate.topics).slice(0, 3)) options.push({ type: 'topic', value: topic })
  }
  const host = candidate.sourceName || sourceHost(candidate.url)
  if (impactDialog.scope === 'source' && host) options.push({ type: 'source', value: host })
  if (impactDialog.scope === 'style' && candidate.contentStyle) options.push({ type: 'style', value: candidate.contentStyle })
  if (impactDialog.scope === 'style' && candidate.contentType && candidate.contentType !== candidate.contentStyle) options.push({ type: 'style', value: candidate.contentType })
  return options
})
const impactDialogTitle = computed(() => ({ source: '屏蔽此来源', topic: '少推荐此主题', style: '少推荐此风格' }[impactDialog.scope]))

const authHeaders = (json = false): Record<string, string> => {
  const token = localStorage.getItem('token') || sessionStorage.getItem('token')
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  }
}

const requestJSON = async <T>(url: string, options: RequestInit = {}): Promise<T> => {
  const response = await fetch(url, options)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.Status === '0') {
    throw new Error(payload.Message || `HTTP ${response.status}`)
  }
  return payload.Data as T
}

const loadToday = async () => {
  todaySnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>('/api/recommendations/today', { headers: authHeaders() }))
  await Promise.all(todaySnapshot.value.items.map(loadItemFeedback))
}

const loadIdentity = async () => {
  const user = await requestJSON<{ role?: string }>('/api/authChecker', { headers: authHeaders() })
  isOwner.value = user?.role === 'owner'
}

const clearCrawlQueueTimer = () => {
  if (crawlQueueTimer !== null) {
    window.clearTimeout(crawlQueueTimer)
    crawlQueueTimer = null
  }
}

const scheduleCrawlQueueRefresh = () => {
  clearCrawlQueueTimer()
  if (!isOwner.value || activeTab.value !== 'queue') return
  const delay = crawlQueue.value.state === 'running' ? 2000 : 10000
  crawlQueueTimer = window.setTimeout(() => { void loadCrawlQueue(true) }, delay)
}

const loadCrawlQueue = async (silent = false) => {
  if (!isOwner.value) return
  const wasRunning = crawlQueue.value.state === 'running'
  try {
    if (!silent) crawlQueueLoading.value = true
    crawlQueue.value = await requestJSON<CrawlQueueSnapshot>('/api/admin/discovery/crawl-queue?limit=50', { headers: authHeaders() })
    if (wasRunning && crawlQueue.value.state !== 'running') {
      await Promise.all([loadSources(), loadCandidates()])
    }
  } catch (error) {
    if (!silent) Message.error(error instanceof Error ? error.message : '加载爬取任务队列失败')
  } finally {
    crawlQueueLoading.value = false
    scheduleCrawlQueueRefresh()
  }
}

const runCrawlQueue = async () => {
  try {
    runningCrawlQueue.value = true
    crawlQueue.value = await requestJSON<CrawlQueueSnapshot>('/api/admin/discovery/crawl-queue/run', { method: 'POST', headers: authHeaders() })
    Message.success('爬取任务队列已开始执行')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '启动爬取任务队列失败')
  } finally {
    runningCrawlQueue.value = false
    scheduleCrawlQueueRefresh()
  }
}

const loadDomainBlacklist = async (silent = false) => {
  if (!isOwner.value) return
  try {
    if (!silent) blacklistLoading.value = true
    domainBlacklist.value = (await requestJSON<DomainBlacklistEntry[]>('/api/admin/discovery/domain-blacklist', { headers: authHeaders() })) ?? []
  } catch (error) {
    if (!silent) Message.error(error instanceof Error ? error.message : '加载域名黑名单失败')
  } finally {
    blacklistLoading.value = false
  }
}

const refreshQueueTab = async () => {
  await Promise.all([loadCrawlQueue(), loadDomainBlacklist()])
}

const addDomainBlacklist = async () => {
  const domain = blacklistForm.domain.trim()
  if (!domain) {
    Message.warning('请输入要屏蔽的域名')
    return
  }
  try {
    addingBlacklist.value = true
    const mutation = await requestJSON<DomainBlacklistMutation>('/api/admin/discovery/domain-blacklist', {
      method: 'POST', headers: authHeaders(true), body: JSON.stringify({ domain, reason: blacklistForm.reason.trim() }),
    })
    blacklistForm.domain = ''
    blacklistForm.reason = ''
    await Promise.all([loadDomainBlacklist(true), loadCrawlQueue(true), loadCandidates()])
    Message.success(`域名已加入黑名单，剔除 ${mutation.affectedCandidates || 0} 篇候选文章`)
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '添加域名黑名单失败')
  } finally {
    addingBlacklist.value = false
  }
}

const deleteDomainBlacklist = async (id: number) => {
  try {
    deletingBlacklistId.value = id
    const mutation = await requestJSON<DomainBlacklistMutation>(`/api/admin/discovery/domain-blacklist/${id}`, { method: 'DELETE', headers: authHeaders() })
    await Promise.all([loadDomainBlacklist(true), loadCrawlQueue(true), loadCandidates()])
    Message.success(`域名黑名单已删除，恢复 ${mutation.affectedCandidates || 0} 篇候选文章`)
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除域名黑名单失败')
  } finally {
    deletingBlacklistId.value = null
  }
}

const loadItemFeedback = async (item: RecommendationItem) => {
  const data = await requestJSON<{ current?: { action: string } }>(`/api/recommendations/items/${item.id}/feedback`, { headers: authHeaders() })
  feedbackByItem[item.id] = data?.current ? { action: data.current.action === 'duplicate' ? 'too_repetitive' : data.current.action } : undefined
}

const loadHistory = async () => {
  historyDays.value = (await requestJSON<RecommendationDay[]>('/api/recommendations/history?page=1&pageSize=30', { headers: authHeaders() })) ?? []
  if (!historyDate.value && historyDays.value.length > 0) {
    historyDate.value = historyDays.value[0].date
  }
  await loadHistoryDay()
}

const loadHistoryDay = async () => {
  if (!historyDate.value) return
  historySnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>(`/api/recommendations/days/${historyDate.value}`, { headers: authHeaders() }))
}

const selectHistoryDay = async (date: string) => {
  historyDate.value = date
  await loadHistoryDay()
}

const loadSettings = async () => {
  const settings = await requestJSON<RecommendationSettings>('/api/recommendations/settings', { headers: authHeaders() })
  Object.assign(settingsForm, settings)
  preferredTopicsText.value = parseList(settings.preferredTopics).join(', ')
  preferredLanguagesText.value = parseList(settings.preferredLanguages).join(', ')
  favoriteSourcesText.value = parseList(settings.favoriteSources).join(', ')
}

const loadBlocks = async () => {
  blockRules.value = (await requestJSON<BlockRule[]>('/api/recommendations/blocks', { headers: authHeaders() })) ?? []
}

const loadRankings = async () => {
  rankings.value = (await requestJSON<ArchiveRankingItem[]>(`/api/archive/rankings?window=${rankingWindow.value}&limit=20`, { headers: authHeaders() })) ?? []
}

const loadKeywords = async () => {
  keywords.value = (await requestJSON<KeywordItem[]>(`/api/search/keywords?window=${keywordWindow.value}&limit=30`, { headers: authHeaders() })) ?? []
}

const loadArchiveRecommendations = async () => {
  archiveRecommendations.value = (await requestJSON<ArchiveRecommendationItem[]>('/api/recommendations/archives?window=7d&limit=20', { headers: authHeaders() })) ?? []
}

const loadSources = async () => {
  sources.value = (await requestJSON<DiscoverySource[]>('/api/discovery/sources', { headers: authHeaders() })) ?? []
}

const loadDiscoveryFeed = async () => {
  try {
    discoveryFeedLoading.value = true
    let snapshot = await requestJSON<RecommendationFeedSnapshot>('/api/recommendations/discovery-feed', { headers: authHeaders() })
    if (!snapshot?.batch) {
      snapshot = await requestJSON<RecommendationFeedSnapshot>('/api/recommendations/discovery-feed/refresh', { method: 'POST', headers: authHeaders() })
    }
    discoveryFeed.value = { batch: snapshot?.batch ?? null, items: snapshot?.items ?? [] }
    await Promise.all(discoveryFeed.value.items.map(loadItemFeedback))
  } finally {
    discoveryFeedLoading.value = false
  }
}

const refreshDiscoveryFeedBatch = async () => {
  try {
    refreshingDiscoveryFeed.value = true
    const snapshot = await requestJSON<RecommendationFeedSnapshot>('/api/recommendations/discovery-feed/refresh', { method: 'POST', headers: authHeaders() })
    discoveryFeed.value = { batch: snapshot?.batch ?? null, items: snapshot?.items ?? [] }
    await Promise.all(discoveryFeed.value.items.map(loadItemFeedback))
    Message.success('已换一批猜你喜欢')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '换一换失败')
  } finally {
    refreshingDiscoveryFeed.value = false
  }
}

const loadCandidates = async () => {
  if (candidateStatus.value === 'new') {
    await loadDiscoveryFeed()
    return
  }
  candidates.value = (await requestJSON<DiscoveryCandidate[]>(`/api/discovery/candidates?status=${candidateStatus.value}&limit=80`, { headers: authHeaders() })) ?? []
}

const loadAll = async () => {
  try {
    loading.value = true
    await Promise.all([
      loadIdentity(),
      loadToday(),
      loadHistory(),
      loadSettings(),
      loadBlocks(),
      loadRankings(),
      loadKeywords(),
      loadArchiveRecommendations(),
      loadSources(),
      loadCandidates(),
    ])
    if (activeTab.value === 'queue' && isOwner.value) await refreshQueueTab()
  } catch (error) {
    Notification.error({ title: '加载失败', content: error instanceof Error ? error.message : '推荐中心加载失败', position: 'topRight' })
  } finally {
    loading.value = false
  }
}

const supplementDaily = async (date: string) => {
  try {
    generating.value = true
    const query = date ? `?date=${encodeURIComponent(date)}` : ''
    todaySnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>(`/api/admin/recommendations/supplement${query}`, { method: 'POST', headers: authHeaders() }))
    await loadHistory()
    Message.success('日报已按缺口追加')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '补充日报失败')
  } finally { generating.value = false }
}

const saveSettings = async () => {
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

const resetPreferences = async () => {
  try {
    savingSettings.value = true
    await requestJSON('/api/recommendations/preferences/reset', { method: 'POST', headers: authHeaders() })
    await Promise.all([loadSettings(), loadBlocks()])
    Message.success('个人推荐偏好已重置，历史日报和反馈事件保留')
  } catch (error) { Message.error(error instanceof Error ? error.message : '重置失败') }
  finally { savingSettings.value = false }
}

const saveSource = async () => {
  if (!sourceForm.url.trim()) {
    Message.warning('请输入内容源 URL')
    return
  }
  try {
    savingSource.value = true
    await requestJSON<DiscoverySource>('/api/discovery/sources', {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ ...sourceForm, enabled: true }),
    })
    sourceForm.name = ''
    sourceForm.url = ''
    sourceForm.type = 'feed'
    await loadSources()
    Message.success('内容源已添加')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '添加内容源失败')
  } finally {
    savingSource.value = false
  }
}

const fetchSource = async (sourceId: number) => {
  try {
    fetchingSourceId.value = sourceId
    await requestJSON(`/api/discovery/sources/${sourceId}/fetch`, { method: 'POST', headers: authHeaders() })
    await Promise.all([loadSources(), loadCandidates()])
    Message.success('内容源刷新完成')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '刷新内容源失败')
  } finally {
    fetchingSourceId.value = null
  }
}

const deleteSource = async (sourceId: number) => {
  try {
    await requestJSON(`/api/discovery/sources/${sourceId}`, { method: 'DELETE', headers: authHeaders() })
    await loadSources()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除内容源失败')
  }
}

const sendFeedback = async (item: RecommendationItem, action: FeedbackAction, blockTargets: BlockTarget[] = []) => {
  try {
    feedbackLoadingId.value = item.id
    await requestJSON(`/api/recommendations/items/${item.id}/feedback`, {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ action, blockTargets }),
    })
    await Promise.all([loadBlocks(), loadItemFeedback(item)])
    Message.success('反馈已记录')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '记录反馈失败')
  } finally {
    feedbackLoadingId.value = null
  }
}

const revertFeedback = async (item: RecommendationItem) => {
  try {
    feedbackLoadingId.value = item.id
    await requestJSON(`/api/recommendations/items/${item.id}/feedback`, { method: 'DELETE', headers: authHeaders() })
    feedbackByItem[item.id] = undefined
    await loadBlocks()
    Message.success('当前反馈已撤销，历史事件仍保留')
  } catch (error) { Message.error(error instanceof Error ? error.message : '撤销反馈失败') }
  finally { feedbackLoadingId.value = null }
}

const openImpactDialog = (item: RecommendationItem, scope: BlockRuleType) => {
  impactDialog.item = item
  impactDialog.scope = scope
  impactDialog.visible = true
  selectedImpactValue.value = impactOptions.value[0]?.value || ''
}

const submitImpactFeedback = async () => {
  if (!impactDialog.item) return
  const targets = impactOptions.value.filter((option) => option.value === selectedImpactValue.value)
  if (targets.length === 0) {
    Message.warning('请选择影响范围')
    return
  }
  const action: FeedbackAction = impactDialog.scope === 'source' ? 'block_source' : impactDialog.scope === 'topic' ? 'reduce_topic' : 'reduce_style'
  await sendFeedback(impactDialog.item, action, targets)
  impactDialog.visible = false
}

const openItemContext = async (item: RecommendationItem) => {
  contextDrawerVisible.value = true
  contextLoading.value = true
  itemContext.value = undefined
  try { itemContext.value = await requestJSON<any>(`/api/recommendations/items/${item.id}/context`, { headers: authHeaders() }) }
  catch (error) { Message.error(error instanceof Error ? error.message : '加载追溯信息失败') }
  finally { contextLoading.value = false }
}

const loadSiteInsight = async (source: DiscoverySource) => {
  selectedSource.value = source
  await reloadSiteInsight(source.siteId || 0)
}

const reloadSiteInsight = async (siteId: number) => {
  if (!siteId) return
  const requests: Promise<void>[] = [
    requestJSON<any>(`/api/discovery/sites/${siteId}/graph`, { headers: authHeaders() }).then((value) => { siteGraph.value = value }),
    requestJSON<any[]>(`/api/discovery/sites/${siteId}/backfill`, { headers: authHeaders() }).then((value) => { siteBackfills.value = value || [] }),
  ]
  if (isOwner.value) requests.push(requestJSON<any>(`/api/discovery/sites/${siteId}/operations`, { headers: authHeaders() }).then((value) => { siteOperations.value = value }))
  try { await Promise.all(requests) } catch (error) { Message.error(error instanceof Error ? error.message : '加载站点详情失败') }
}

const requestBackfill = async (siteId: number) => {
  try {
    await requestJSON(`/api/discovery/sites/${siteId}/backfill`, { method: 'POST', headers: authHeaders() })
    await reloadSiteInsight(siteId)
    Message.success('历史回溯已排队')
  } catch (error) { Message.error(error instanceof Error ? error.message : '启动回溯失败') }
}

const requestSitemapBackfill = async (siteId: number, url: string) => {
  try {
    await requestJSON(`/api/discovery/sites/${siteId}/sitemap-backfill`, {
      method: 'POST', headers: authHeaders(true), body: JSON.stringify({ url }),
    })
    await reloadSiteInsight(siteId)
    Message.success('Sitemap 历史补漏已排队')
  } catch (error) { Message.error(error instanceof Error ? error.message : '启动 Sitemap 补漏失败') }
}

const deleteBlockRule = async (ruleId: number) => {
  try {
    await requestJSON(`/api/recommendations/blocks/${ruleId}`, { method: 'DELETE', headers: authHeaders() })
    await loadBlocks()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除屏蔽规则失败')
  }
}

const openArchive = (path: string) => {
  router.push({ path: '/htmlviewer', query: { loc: path } })
}

const searchKeyword = (keyword: string) => {
  router.push({ path: '/search', query: { q: keyword } })
}

const markCandidateRead = async (candidate: DiscoveryCandidate) => {
  if (!candidate?.url) return
  try {
    await requestJSON(`/api/discovery/candidates/${candidate.id}/read`, { method: 'POST', headers: authHeaders() })
    await loadCandidates()
  } catch { /* Opening the article should not depend on recording read state. */ }
}

const openCandidate = (candidate: DiscoveryCandidate) => {
  if (!candidate?.url) return
  window.open(candidate.url, '_blank', 'noopener,noreferrer')
  void markCandidateRead(candidate)
}

const archiveCandidate = async (candidateId: number) => {
  try {
    archivingCandidateId.value = candidateId
    await requestJSON(`/api/discovery/candidates/${candidateId}/archive`, { method: 'POST', headers: authHeaders() })
    await loadCandidates()
    Message.success('已加入归档队列')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '加入归档失败')
  } finally {
    archivingCandidateId.value = null
  }
}

const ignoreCandidate = async (candidateId: number) => {
  try {
    await requestJSON(`/api/discovery/candidates/${candidateId}/ignore`, { method: 'POST', headers: authHeaders() })
    await loadCandidates()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '忽略候选失败')
  }
}

function emptySnapshot(date: string): RecommendationSnapshot {
  return {
    day: { id: 0, userId: 0, date, timezone: 'Asia/Shanghai', status: 'missing', requestedCount: 10, actualCount: 0 },
    items: [],
  }
}

function normalizeSnapshot(snapshot: RecommendationSnapshot | null | undefined): RecommendationSnapshot {
  const fallback = emptySnapshot(formatLocalDate(new Date()))
  if (!snapshot?.day) return fallback
  return { day: snapshot.day, items: snapshot.items ?? [] }
}

function formatLocalDate(date: Date): string {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}-${month}-${day}`
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function parseList(raw?: string): string[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((item) => typeof item === 'string' && item.trim()).map((item) => item.trim()) : []
  } catch {
    return []
  }
}

function splitPreference(raw: string): string[] {
  return [...new Set(raw.split(/[,，\n]/).map((value) => value.trim()).filter(Boolean))]
}

function sourceHost(rawUrl?: string): string {
  if (!rawUrl) return ''
  try {
    return new URL(rawUrl).hostname
  } catch {
    return rawUrl
  }
}

function blockRuleTypeLabel(type: string): string {
  if (type === 'topic') return '主题'
  if (type === 'source') return '来源'
  if (type === 'style') return '类型'
  return type
}

function crawlTaskKindLabel(kind: string): string {
  return ({
    discovery_fetch_source: '订阅源抓取',
    discovery_scan_blogroll: 'Blogroll 扫描',
    discovery_backfill_site: '历史回溯',
    discovery_process_candidate: '文章正文处理',
  } as Record<string, string>)[kind] || kind
}

function crawlTaskStatusLabel(status: CrawlTaskStatus): string {
  return ({ pending: '等待', running: '运行中', succeeded: '成功', failed: '失败' } as Record<CrawlTaskStatus, string>)[status]
}

function crawlTaskTargetLabel(task: CrawlQueueTask): string {
  const type = ({ source: '来源', site: '站点', candidate: '候选文章' } as Record<string, string>)[task.targetType] || '目标'
  const version = task.contentVersion ? ` · 正文 v${task.contentVersion}` : ''
  return `${type} #${task.targetId || '-'}${version}`
}

function crawlTaskTimeLabel(task: CrawlQueueTask): string {
  const value = task.finishedAt || task.startedAt || task.scheduledAt || task.createdAt
  return value ? formatDateTime(value) : '暂无时间'
}

function crawlTaskGroupTimeLabel(item: { windowStart: string; windowEnd: string }): string {
  const start = formatDateTime(item.windowStart)
  const end = formatDateTime(item.windowEnd)
  return start === end ? start : `${start} 至 ${end}`
}

watch(activeTab, (tab) => {
  if (tab === 'queue' && isOwner.value) {
    void refreshQueueTab()
    return
  }
  clearCrawlQueueTimer()
})

onMounted(loadAll)
onBeforeUnmount(clearCrawlQueueTimer)
</script>

<style scoped>
.recommendations-view {
  min-height: 100vh;
  padding: 20px;
  background: #f7f8fa;
  color: #1d2129;
  position: relative;
}

.top-bar {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
}

.back-button {
  color: #4e5969;
  background: #ffffff;
  border-radius: 8px;
  box-shadow: 0 8px 22px rgba(29, 33, 41, 0.08);
}

.content {
  width: min(1180px, 100%);
  margin: 0 auto;
  padding: 72px 0 40px;
}

.page-header {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 18px;
  margin-bottom: 18px;
  padding: 20px 22px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 12px 30px rgba(29, 33, 41, 0.06);
}

.header-mark {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: #165dff;
  color: #ffffff;
  font-size: 28px;
}

.page-header h1 {
  margin: 0 0 6px;
  font-size: 28px;
  color: #111827;
}

.page-header p {
  margin: 0;
  color: #64748b;
  line-height: 1.5;
}

.tabs {
  padding: 18px 22px 24px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 12px 30px rgba(29, 33, 41, 0.06);
}

.panel {
  padding: 20px 0;
}

.section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.section-title h2 {
  margin: 0 0 4px;
  font-size: 20px;
}

.section-title span,
.item-meta,
.compact-card span,
.source-row span,
.source-row small,
.recommendation-card small {
  color: #86909c;
  font-size: 13px;
}

.recommendation-list,
.item-list,
.source-list,
.history-detail {
  display: grid;
  gap: 12px;
}

.discovery-feed-spin {
  display: block;
  width: 100%;
  min-height: 180px;
}

.feed-shortage {
  margin: 14px 0 0;
  color: #86909c;
  font-size: 13px;
  text-align: center;
}

.feed-refresh-row {
  display: flex;
  justify-content: center;
  margin-top: 18px;
}

.recommendation-card,
.compact-card,
.source-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 18px;
  min-height: 126px;
  padding: 16px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
}

.item-main {
  min-width: 0;
}

.item-main h3,
.source-row h3 {
  margin: 0 0 8px;
  font-size: 17px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.candidate-title-link {
  color: #1d2129;
  text-decoration: none;
  transition: color 0.2s ease;
}

.candidate-title-link:hover,
.candidate-title-link:focus-visible {
  color: #165dff;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.item-main p,
.source-row p {
  min-height: 44px;
  display: -webkit-box;
  margin: 0 0 8px;
  overflow: hidden;
  color: #4e5969;
  line-height: 1.55;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.item-meta,
.topic-row,
.feedback-row,
.item-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.item-meta {
  margin-bottom: 8px;
}

.topic-row {
  min-height: 24px;
  margin: 8px 0;
}

.topic-row span {
  padding: 2px 8px;
  border-radius: 6px;
  background: #eef2ff;
  color: #1d4ed8;
  font-size: 12px;
}

.item-actions {
  width: 236px;
  justify-content: flex-end;
}

.feedback-row {
  justify-content: flex-end;
}

.history-layout {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 16px;
}

.day-list {
  display: grid;
  align-content: start;
  gap: 8px;
}

.day-list button,
.keyword-tile {
  display: grid;
  gap: 6px;
  padding: 12px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  color: #1d2129;
  cursor: pointer;
  text-align: left;
}

.day-list button.active,
.day-list button:hover,
.keyword-tile:hover {
  border-color: #165dff;
  box-shadow: inset 3px 0 0 #165dff;
}

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

.source-panel {
  border-bottom: 1px solid #e5e6eb;
}

.queue-state {
  padding: 5px 10px;
  border-radius: 999px;
  background: #f2f3f5;
  color: #4e5969;
  font-size: 13px;
}

.queue-state-running {
  background: #e8f3ff;
  color: #165dff;
}

.queue-state-waiting {
  background: #fff7e8;
  color: #d46b08;
}

.queue-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.queue-summary > div {
  display: grid;
  gap: 4px;
  padding: 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #f7f8fa;
}

.queue-summary strong {
  font-size: 24px;
}

.queue-summary span,
.queue-task-row span,
.queue-task-row small {
  color: #86909c;
  font-size: 12px;
}

.queue-task-list {
  display: grid;
  gap: 8px;
}

.queue-task-row {
  display: grid;
  grid-template-columns: minmax(180px, 1.4fr) 90px 90px minmax(150px, 1fr);
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

.queue-task-row > div {
  display: grid;
  gap: 4px;
}

.queue-task-summary {
  border-style: dashed;
  background: #f7f8fa;
}

.queue-task-summary strong {
  color: #4e5969;
}

.task-status {
  width: fit-content;
  padding: 3px 8px;
  border-radius: 999px;
  background: #f2f3f5;
}

.task-status-running { background: #e8f3ff; color: #165dff !important; }
.task-status-succeeded { background: #e8ffea; color: #00a854 !important; }
.task-status-failed { background: #ffece8; color: #f53f3f !important; }
.queue-task-error { grid-column: 1 / -1; color: #f53f3f !important; overflow-wrap: anywhere; }

.domain-blacklist-panel {
  border-top: 1px solid #e5e6eb;
}

.blacklist-form {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(280px, 2fr) auto;
  align-items: end;
  gap: 12px;
  margin-bottom: 16px;
}

.blacklist-list {
  display: grid;
  gap: 8px;
}

.blacklist-row {
  display: grid;
  grid-template-columns: minmax(220px, 1.2fr) minmax(220px, 1fr) auto;
  align-items: center;
  gap: 14px;
  padding: 12px 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

.blacklist-row > div {
  display: grid;
  gap: 4px;
}

.blacklist-row span {
  color: #86909c;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.source-form {
  display: grid;
  grid-template-columns: minmax(160px, 1fr) minmax(280px, 2fr) minmax(110px, 130px) auto;
  align-items: end;
  gap: 12px;
  margin-bottom: 16px;
}

.source-field,
.inline-field {
  display: grid;
  gap: 6px;
  color: #4e5969;
  font-size: 13px;
}

.source-field input,
.source-field select,
.inline-field input,
.inline-field select {
  height: 32px;
  padding: 0 10px;
  border: 1px solid #c9cdd4;
  border-radius: 6px;
  background: #ffffff;
  color: #1d2129;
}

.source-field input:focus,
.source-field select:focus,
.inline-field input:focus,
.inline-field select:focus {
  border-color: #165dff;
  outline: none;
  box-shadow: 0 0 0 2px rgba(22, 93, 255, 0.12);
}

.toggle-row,
.block-row,
.block-options label {
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

.block-options {
  display: grid;
  gap: 12px;
}

.keyword-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}

@media (max-width: 820px) {
  .page-header,
  .recommendation-card,
  .compact-card,
  .source-row,
  .history-layout,
  .settings-grid,
  .source-form,
  .blacklist-form,
  .blacklist-row {
    grid-template-columns: 1fr;
  }

  .queue-summary,
  .queue-task-row {
    grid-template-columns: 1fr 1fr;
  }

  .section-title {
    align-items: flex-start;
    flex-direction: column;
  }

  .item-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .top-bar {
    position: static;
    display: flex;
    justify-content: flex-end;
    margin-bottom: 12px;
  }

  .content {
    padding-top: 0;
  }
}
</style>
