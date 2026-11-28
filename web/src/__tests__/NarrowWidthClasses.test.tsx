import { afterEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MetricsKpiRow } from '../components/metrics/metricsShared';
import { TemplateGrid } from '../components/wizard/TemplateGrid';
import { BrowseStep } from '../components/wizard/steps/BrowseStep';
import { StackForm } from '../components/wizard/steps/StackForm';
import { CatalogPicker } from '../components/wizard/CatalogPicker';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { ToastContainer, showToast } from '../components/ui/Toast';
import { useWizardStore } from '../stores/useWizardStore';
import { useStackStore } from '../stores/useStackStore';
import type { SkillPreview } from '../types';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchWizardDrafts: vi.fn().mockResolvedValue([]),
  fetchCatalog: vi.fn().mockResolvedValue({ query: '', source: 'all', servers: [] }),
}));

afterEach(() => {
  cleanup();
});

describe('Metrics KPI row', () => {
  it('reflows from two to four columns inside its own query container', () => {
    render(
      <MetricsKpiRow
        kpis={{ input: 1, output: 2, total: 3, savingsPercent: 0, savedTokens: 0 }}
        windowTotals={{ input: 1, output: 2, total: 3, isEmpty: false }}
        windowLabel="Last hour"
      />,
    );
    const grid = screen.getByText('Input Tokens').closest('.grid');
    expect(grid).toHaveClass('grid-cols-2', '@2xl:grid-cols-4');
    expect(grid).not.toHaveClass('grid-cols-4');
    expect(grid?.parentElement).toHaveClass('@container');
    expect(screen.getByText('Output Tokens')).toBeInTheDocument();
    expect(screen.getByText('Total Tokens')).toBeInTheDocument();
    expect(grid?.children).toHaveLength(4);
  });
});

describe('Wizard grids', () => {
  it('type picker steps from one to three columns', () => {
    useWizardStore.getState().reset();
    useStackStore.setState({ stackName: 'demo' });
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);
    const grid = screen.getByText('MCP Server').closest('.grid');
    expect(grid).toHaveClass('grid-cols-1', 'min-[400px]:grid-cols-2', 'md:grid-cols-3');
  });

  it('template grid is one column below sm', () => {
    render(<TemplateGrid resourceType="mcp-server" selected={null} onSelect={() => {}} />);
    const grid = screen.getByText('Blank Server').closest('.grid');
    expect(grid).toHaveClass('grid-cols-1', 'sm:grid-cols-2');
  });

  it('stack resource presets query their own container', () => {
    render(<StackForm data={{ name: '', version: '1' }} onChange={() => {}} />);
    fireEvent.click(screen.getByText('Resources'));
    fireEvent.click(screen.getByText('Add Resource'));
    const grid = screen.getByText('PostgreSQL').closest('.grid');
    expect(grid).toHaveClass('grid-cols-2', '@xs:grid-cols-3', '@md:grid-cols-5');
    expect(grid?.parentElement).toHaveClass('@container', 'mb-3');
  });

  it('browse step stacks the list above the preview below md', () => {
    const previews: SkillPreview[] = [
      { name: 'alpha', description: 'First', body: '# Alpha', valid: true, exists: false },
    ];
    render(<BrowseStep previews={previews} selected={new Set()} onSelectionChange={() => {}} />);
    const grid = document.querySelector('.md\\:grid-cols-5');
    expect(grid).toHaveClass('grid', 'grid-cols-1', 'md:min-h-[280px]');
    expect(grid).not.toHaveClass('min-h-[280px]');
    const [list, preview] = Array.from(grid!.children);
    expect(list).toHaveClass('md:col-span-2', 'max-h-56', 'md:max-h-none');
    expect(preview).toHaveClass('md:col-span-3');
  });

  it('catalog picker stacks its panes in a narrow container', async () => {
    const { container } = render(<CatalogPicker onSelect={() => {}} />);
    await act(async () => {});
    const root = container.firstElementChild;
    expect(root).toHaveClass('@container');
    const row = container.querySelector('.\\@2xl\\:flex-row');
    expect(row).toHaveClass('flex', 'flex-col', 'flex-1', 'min-h-0');
    const [list, preview] = Array.from(row!.children);
    expect(list).toHaveClass('w-full', '@2xl:w-1/2', 'basis-2/5', 'border-b', '@2xl:border-r');
    expect(preview).toHaveClass('w-full', '@2xl:w-1/2', 'flex-1', 'min-h-0');
  });
});

describe('Toast stack', () => {
  it('caps width to the viewport and height in a narrow window', () => {
    render(<ToastContainer />);
    act(() => {
      showToast('success', 'A long message that should wrap inside a narrow window');
    });
    const region = screen.getByRole('region', { name: 'Notifications' });
    expect(region).toHaveClass(
      'max-w-[min(24rem,calc(100vw-2rem))]',
      'max-sm:max-h-[33vh]',
      'max-sm:overflow-y-auto',
      'max-sm:max-w-[calc(100vw-1rem)]',
      'max-sm:p-2',
      'max-sm:-m-2',
      'fixed',
      'bottom-10',
      'right-4',
      'z-toast',
    );
    expect(region).not.toHaveClass('max-w-sm');
    const message = screen.getByText(/A long message/);
    expect(message).toHaveClass('min-w-0', 'break-words');
    expect(message.closest('.glass-panel-elevated')).toHaveClass('max-w-[min(24rem,calc(100vw-2rem))]');
  });

  it('scrolls the newest toast into view when the list changes', () => {
    render(<ToastContainer />);
    const region = screen.getByRole('region', { name: 'Notifications' });
    Object.defineProperty(region, 'scrollHeight', { configurable: true, value: 500 });
    act(() => {
      showToast('success', 'Newest toast');
    });
    expect(region.scrollTop).toBe(500);
  });
});
