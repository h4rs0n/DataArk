import { createRouter, createWebHashHistory, type RouteLocationNormalized } from 'vue-router'
import { ApiResponseError } from '@/api/client'
import { checkAuth } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHashHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'index',
      component: () => import('@/views/IndexView.vue')
    },
    {
      path: '/search',
      name: 'search',
      component: () => import('@/views/SearchView.vue')
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue')
    },
    {
      path: '/archive',
      name: 'archive',
      component: () => import('@/views/ArchiveUrlView.vue')
    },
    {
      path: '/archive-url',
      redirect: '/archive'
    },
    {
      path: '/upload',
      redirect: '/archive'
    },
    {
      path: '/stats',
      name: 'stats',
      component: () => import('@/views/StatsView.vue')
    },
    {
      path: '/recommendations',
      name: 'recommendations',
      component: () => import('@/views/RecommendationsView.vue')
    },
    {
      path: '/backup',
      name: 'backup',
      component: () => import('@/views/BackupView.vue')
    },
    {
      path: '/consistency',
      name: 'consistency',
      component: () => import('@/views/ConsistencyView.vue')
    },
    {
      path: '/htmlviewer',
      name: 'HtmlViewer',
      component: () => import('@/views/HtmlView.vue')
    }
  ]
})

router.beforeEach(async (to: RouteLocationNormalized) => {
  if (to.path === '/login') {
    return true
  }

  const auth = useAuthStore()
  if (!auth.token) {
    return '/login'
  }

  try {
    await checkAuth()
  } catch (error) {
    if (error instanceof ApiResponseError && error.statusCode === 401) {
      auth.clearAuth()
      return '/login'
    }
    // 瞬时网络错误不踢出当前路由。
  }

  return true
})

export default router
