import { createRouter, createWebHashHistory, type RouteLocationNormalized } from 'vue-router'

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
      path: '/backup',
      name: 'backup',
      component: () => import('@/views/BackupView.vue')
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

  const token = localStorage.getItem('token')
  if (!token) {
    return '/login'
  }

  try {
    const response = await fetch('/api/authChecker', {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`
      }
    })

    if (response.status === 401) {
      localStorage.removeItem('token')
      sessionStorage.removeItem('token')
      return '/login'
    }
  } catch {
    // Keep current route behavior on transient network errors.
  }

  return true
})

export default router
