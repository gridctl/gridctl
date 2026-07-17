import { describe, it, expect } from 'vitest'
import { documentTitleForWorkspace } from '../types/workspace'

describe('smoke test', () => {
  it('vitest runs successfully', () => {
    expect(1 + 1).toBe(2)
  })
})

describe('documentTitleForWorkspace', () => {
  it('returns the base name for a non-workspace path', () => {
    expect(documentTitleForWorkspace(null)).toBe('Gridctl')
  })

  it('appends the workspace label', () => {
    expect(documentTitleForWorkspace('topology')).toBe('Gridctl - Stack')
  })

  it('uses the display label, not the id, for vault', () => {
    expect(documentTitleForWorkspace('vault')).toBe('Gridctl - Variables')
  })
})
