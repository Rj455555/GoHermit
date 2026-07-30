import {
  Bot,
  Gauge,
  ListChecks,
  Settings,
  Users,
  Workflow,
  type LucideIcon,
} from 'lucide-react'

export interface NavigationItem {
  to: string
  labelKey: string
  titleKey: string
  icon: LucideIcon
}

export const navigationItems: NavigationItem[] = [
  {
    to: '/dashboard',
    labelKey: 'navigation.dashboard',
    titleKey: 'pages.dashboard.title',
    icon: Gauge,
  },
  {
    to: '/employees',
    labelKey: 'navigation.employees',
    titleKey: 'pages.employees.title',
    icon: Users,
  },
  {
    to: '/tasks',
    labelKey: 'navigation.tasks',
    titleKey: 'pages.tasks.title',
    icon: ListChecks,
  },
  {
    to: '/agent',
    labelKey: 'navigation.agent',
    titleKey: 'pages.agent.title',
    icon: Bot,
  },
  {
    to: '/loops',
    labelKey: 'navigation.loops',
    titleKey: 'pages.loops.title',
    icon: Workflow,
  },
  {
    to: '/settings',
    labelKey: 'navigation.settings',
    titleKey: 'pages.settings.title',
    icon: Settings,
  },
]

export function getRouteTitleKey(pathname: string): string {
  const item = navigationItems.find(
    ({ to }) => pathname === to || pathname.startsWith(`${to}/`),
  )
  return item?.titleKey ?? 'notFound.title'
}

export function isAgentRoute(pathname: string): boolean {
  return pathname === '/agent' || /^\/agent\/sessions\/[^/]+$/.test(pathname)
}
