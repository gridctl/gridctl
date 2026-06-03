import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { SourceGroupHeader } from '../components/registry/SourceGroupHeader';
import { updateSkillSource } from '../lib/api';
import type { SkillSourceStatus } from '../types';

vi.mock('../lib/api', () => ({ updateSkillSource: vi.fn().mockResolvedValue({ source: 'src', results: [] }) }));
vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const updateMock = vi.mocked(updateSkillSource);

function makeSource(overrides: Partial<SkillSourceStatus> = {}): SkillSourceStatus {
  return {
    name: 'src',
    repo: 'https://github.com/org/repo',
    autoUpdate: false,
    updateInterval: '',
    skills: [],
    updateAvailable: true,
    ...overrides,
  };
}

beforeEach(() => {
  updateMock.mockClear();
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    configurable: true,
    get() {
      return this.parentNode;
    },
  });
});
afterEach(() => cleanup());

describe('SourceGroupHeader drift-aware sync', () => {
  it('syncs immediately (no confirm) when there are no local edits', async () => {
    render(
      <SourceGroupHeader source={makeSource()} count={1} hasSearch={false} isActive={false} onToggle={() => {}} />,
    );
    fireEvent.click(screen.getByTitle('Update available, pull latest'));
    await waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1));
    // Clean sync sends no force flag.
    expect(updateMock).toHaveBeenCalledWith('src', undefined);
  });

  it('opens a confirm listing drifted skills instead of syncing immediately', () => {
    render(
      <SourceGroupHeader
        source={makeSource({ driftedSkills: ['edited-skill'] })}
        count={1}
        hasSearch={false}
        isActive={false}
        onToggle={() => {}}
      />,
    );
    fireEvent.click(screen.getByTitle('Update available, pull latest'));
    expect(updateMock).not.toHaveBeenCalled();
    expect(screen.getByText('edited-skill')).toBeInTheDocument();
    expect(screen.getByText(/local edits that a/i)).toBeInTheDocument();
  });

  it('"keep my edits" syncs without force; "overwrite" syncs with force', async () => {
    const { rerender } = render(
      <SourceGroupHeader
        source={makeSource({ driftedSkills: ['edited-skill'] })}
        count={1}
        hasSearch={false}
        isActive={false}
        onToggle={() => {}}
      />,
    );
    fireEvent.click(screen.getByTitle('Update available, pull latest'));
    fireEvent.click(screen.getByRole('button', { name: /keep my edits/i }));
    await waitFor(() => expect(updateMock).toHaveBeenCalledWith('src', undefined));

    updateMock.mockClear();
    rerender(
      <SourceGroupHeader
        source={makeSource({ driftedSkills: ['edited-skill'] })}
        count={1}
        hasSearch={false}
        isActive={false}
        onToggle={() => {}}
      />,
    );
    fireEvent.click(screen.getByTitle('Update available, pull latest'));
    fireEvent.click(screen.getByRole('button', { name: /overwrite local edits/i }));
    await waitFor(() => expect(updateMock).toHaveBeenCalledWith('src', { force: true }));
  });
});
