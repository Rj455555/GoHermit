import '@testing-library/jest-dom/vitest'

import { afterEach, beforeEach, vi } from 'vitest'

// jsdom has no PointerEvent constructor; testing-library would otherwise fall
// back to a plain Event and drop button/clientX/isPrimary. A MouseEvent-based
// polyfill keeps pointer-event init fields readable in tests.
if (typeof window.PointerEvent !== 'function') {
  class PointerEventPolyfill extends MouseEvent {
    readonly pointerId: number
    readonly pointerType: string
    readonly isPrimary: boolean

    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init)
      this.pointerId = init.pointerId ?? 0
      this.pointerType = init.pointerType ?? 'mouse'
      this.isPrimary = init.isPrimary ?? true
    }
  }
  Object.defineProperty(window, 'PointerEvent', {
    value: PointerEventPolyfill,
    writable: true,
    configurable: true,
  })
}

// jsdom has no layout engine, so elementFromPoint is absent. Provide a null
// default that individual tests can override with vi.spyOn.
if (typeof document.elementFromPoint !== 'function') {
  Object.defineProperty(document, 'elementFromPoint', {
    value: () => null,
    writable: true,
    configurable: true,
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => undefined)))
  localStorage.clear()
  document.documentElement.lang = ''
  document.title = ''
  document.body.style.overflow = ''
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
})
