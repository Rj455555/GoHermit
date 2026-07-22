import type { Locator, Page } from '@playwright/test'

export class AgentPage {
  readonly page: Page
  readonly task: Locator
  readonly send: Locator
  readonly approvePlan: Locator
  readonly planSteps: Locator
  readonly approvalPanel: Locator
  readonly approvalCards: Locator

  constructor(page: Page) {
    this.page = page
    this.task = page.locator('#task')
    this.send = page.locator('#send')
    this.approvePlan = page.locator('#approve-plan')
    this.planSteps = page.locator('#plan-steps .plan-step')
    this.approvalPanel = page.locator('#approval-panel')
    this.approvalCards = page.locator('#approval-list .approval-card')
  }

  async goto() {
    await this.page.goto('/')
    await this.page.locator('#company').waitFor({ state: 'attached' })
  }

  async startReviewTask(message: string) {
    await this.page.locator('#plan-mode').selectOption('review')
    await this.task.fill(message)
    await this.send.click()
  }
}
