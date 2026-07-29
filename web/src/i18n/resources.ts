export const DEFAULT_LOCALE = 'zh-CN'
export const SUPPORTED_LOCALES = ['zh-CN', 'en-US'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]

export const translationResources = {
  'zh-CN': {
    translation: {
      actions: {
        backDashboard: '返回仪表盘',
        cancel: '取消',
        confirm: '确认',
        dismiss: '关闭',
        retry: '重试',
      },
      brand: {
        label: 'GOHERMIT · 工作流',
      },
      dialog: {
        sampleDescription: '此操作需要您的确认。',
        sampleTitle: '确认操作',
      },
      errorBoundary: {
        description: '当前页面暂时无法显示。Shell 导航仍可使用。',
        title: '页面出现问题',
      },
      language: {
        label: '语言',
        toChinese: '切换到简体中文',
        toEnglish: '切换到 English',
      },
      navigation: {
        agent: '智能体',
        collapse: '收起主导航',
        dashboard: '仪表盘',
        employees: '电子员工',
        expand: '展开主导航',
        label: '主导航',
        loops: '工作流',
        settings: '设置',
        tasks: '任务',
      },
      notFound: {
        description: '该 React 路由未声明或地址不正确。',
        title: '页面未找到',
      },
      pages: {
        agent: {
          description: '智能体与会话功能将在 Phase 3 接入。',
          title: '智能体',
        },
        dashboard: {
          description: '仪表盘功能将在 Phase 3 接入。',
          title: '仪表盘',
        },
        employees: {
          description: '电子员工功能将在 Phase 4 接入。',
          title: '电子员工',
        },
        loops: {
          description: '工作流功能将在 Phase 4 接入。',
          title: '工作流',
        },
        settings: {
          description: '设置功能将在 Phase 3 接入。',
          title: '设置',
        },
        tasks: {
          description: '任务功能将在 Phase 4 接入。',
          title: '任务',
        },
      },
      session: {
        closeDrawer: '关闭会话抽屉',
        collapse: '收起会话栏',
        done: '完成',
        expand: '展开会话栏',
        label: '会话',
        openDrawer: '打开会话抽屉',
        placeholder: '会话功能将在 Phase 3 接入',
      },
      status: {
        ready: '就绪',
      },
      toast: {
        saved: '已保存',
      },
    },
  },
  'en-US': {
    translation: {
      actions: {
        backDashboard: 'Back to Dashboard',
        cancel: 'Cancel',
        confirm: 'Confirm',
        dismiss: 'Dismiss',
        retry: 'Retry',
      },
      brand: {
        label: 'GOHERMIT · LOOP WORKBENCH',
      },
      dialog: {
        sampleDescription: 'This action needs your confirmation.',
        sampleTitle: 'Confirm action',
      },
      errorBoundary: {
        description: 'This page cannot be displayed right now. Shell navigation remains available.',
        title: 'Page error',
      },
      language: {
        label: 'Language',
        toChinese: 'Switch to 简体中文',
        toEnglish: 'Switch to English',
      },
      navigation: {
        agent: 'Agent',
        collapse: 'Collapse navigation',
        dashboard: 'Dashboard',
        employees: 'Employees',
        expand: 'Expand navigation',
        label: 'Primary navigation',
        loops: 'Loops',
        settings: 'Settings',
        tasks: 'Tasks',
      },
      notFound: {
        description: 'This React route is not declared or the address is invalid.',
        title: 'Page not found',
      },
      pages: {
        agent: {
          description: 'Agent and Session features will be connected in Phase 3.',
          title: 'Agent',
        },
        dashboard: {
          description: 'Dashboard features will be connected in Phase 3.',
          title: 'Dashboard',
        },
        employees: {
          description: 'Employee features will be connected in Phase 4.',
          title: 'Employees',
        },
        loops: {
          description: 'Loop features will be connected in Phase 4.',
          title: 'Loops',
        },
        settings: {
          description: 'Settings features will be connected in Phase 3.',
          title: 'Settings',
        },
        tasks: {
          description: 'Task features will be connected in Phase 4.',
          title: 'Tasks',
        },
      },
      session: {
        closeDrawer: 'Close Session drawer',
        collapse: 'Collapse Session sidebar',
        done: 'Done',
        expand: 'Expand Session sidebar',
        label: 'Sessions',
        openDrawer: 'Open Session drawer',
        placeholder: 'Session features will be connected in Phase 3',
      },
      status: {
        ready: 'Ready',
      },
      toast: {
        saved: 'Saved',
      },
    },
  },
} as const

type TranslationBranch = string | { readonly [key: string]: TranslationBranch }

export function flattenLeafKeys(branch: TranslationBranch, prefix = ''): string[] {
  if (typeof branch === 'string') return [prefix]
  return Object.entries(branch)
    .flatMap(([key, value]) => flattenLeafKeys(value, prefix ? `${prefix}.${key}` : key))
    .sort()
}
