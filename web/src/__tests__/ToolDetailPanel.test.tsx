import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { ToolDetailPanel } from '../components/workspaces/ToolDetailPanel';
import type { ToolRow } from '../hooks/useToolsEditor';

const TOOL: ToolRow = {
  name: 'getThing',
  description: 'Fetch a thing by id',
} as ToolRow;

const SCHEMA = { type: 'object', properties: { id: { type: 'string' } } };

function renderPanel(overrides = {}) {
  return render(
    <ToolDetailPanel
      serverName="atlassian"
      tool={TOOL}
      schema={SCHEMA}
      enabled
      auditMode={false}
      auditState={null}
      onClose={() => {}}
      {...overrides}
    />,
  );
}

beforeEach(() => {
  localStorage.clear();
});

describe('ToolDetailPanel', () => {
  it('shows an empty state when no tool is selected', () => {
    render(
      <ToolDetailPanel
        serverName="atlassian"
        tool={null}
        enabled={false}
        auditMode={false}
        auditState={null}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText(/select a tool to view/i)).toBeInTheDocument();
    // No control without a tool.
    expect(screen.queryByTitle(/increase font size/i)).not.toBeInTheDocument();
  });

  it('renders the description, schema, and the text-size control', () => {
    renderPanel();
    expect(screen.getByText('Fetch a thing by id')).toBeInTheDocument();
    expect(screen.getByTitle(/increase font size/i)).toBeInTheDocument();
    // Default reflects the pane default (12px).
    expect(screen.getByText('12px')).toBeInTheDocument();
  });

  it('scales the displayed size when zooming in', () => {
    renderPanel();
    fireEvent.click(screen.getByTitle(/increase font size/i));
    expect(screen.getByText('13px')).toBeInTheDocument();
    expect(localStorage.getItem('gridctl-tools-zoom')).toBe('13');
  });

  it('shows the Usage section with calls, tokens, and cost outside audit mode', () => {
    renderPanel({
      usage: { calls: 12, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 1200, outputTokens: 400, costUsd: 0.0345 },
    });
    expect(screen.getByText('Usage')).toBeInTheDocument();
    expect(screen.getByText('Calls')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('1.2k')).toBeInTheDocument();
    expect(screen.getByText('400')).toBeInTheDocument();
    // Priced usage shows an estimated cost with the est. label.
    expect(screen.getByText('Cost · est.')).toBeInTheDocument();
    expect(screen.getByText('$0.035')).toBeInTheDocument();
  });

  it('renders an em dash, never $0, when usage is unpriced', () => {
    renderPanel({ usage: { calls: 3, inputTokens: 100, outputTokens: 50 } });
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.queryByText(/\$0\.00/)).not.toBeInTheDocument();
  });

  it('hides the Usage section without usage data or audit state', () => {
    renderPanel();
    expect(screen.queryByText('Usage')).not.toBeInTheDocument();
  });
});
