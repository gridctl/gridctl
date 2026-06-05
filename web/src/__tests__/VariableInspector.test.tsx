import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariableInspector } from '../components/vault/VariableInspector';
import type { Consumer, Variable } from '../lib/api';

const SECRET_VAR: Variable = {
  key: 'API_KEY',
  type: 'string',
  is_secret: true,
};

const PLAIN_VAR: Variable = {
  key: 'IMAGE_TAG',
  type: 'string',
  is_secret: false,
  set: 'dev',
};

const CONSUMERS: Consumer[] = [
  { kind: 'mcp-server', name: 'github', field: 'env.API_KEY' },
  { kind: 'gateway', field: 'auth.token' },
];

function makeProps(overrides: Partial<Parameters<typeof VariableInspector>[0]> = {}) {
  return {
    variable: null as Variable | null,
    consumers: [] as Consumer[],
    allVariables: [SECRET_VAR, PLAIN_VAR],
    usage: { API_KEY: CONSUMERS },
    setNames: ['dev', 'prod'],
    locked: false,
    getValue: vi.fn().mockResolvedValue({ value: 's3cret-value' }),
    onUpdate: vi.fn().mockResolvedValue(undefined),
    onAssignSet: vi.fn(),
    onDelete: vi.fn(),
    onConsumerClick: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  };
}

// Flush the microtask queue so reveal/copy promise chains settle.
async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('VariableInspector — overview state', () => {
  it('renders the overview with stats when nothing is selected', () => {
    render(<VariableInspector {...makeProps()} />);
    expect(screen.getByText('Variables overview')).toBeInTheDocument();
    // 2 variables, 1 secret, 1 plaintext, 1 unreferenced (IMAGE_TAG).
    expect(screen.getByText('Secrets')).toBeInTheDocument();
    expect(screen.getByText('Unreferenced')).toBeInTheDocument();
    expect(screen.getByText(/drop a \.env or \.json file/i)).toBeInTheDocument();
  });

  it('notes the locked vault instead of stats', () => {
    render(
      <VariableInspector {...makeProps({ allVariables: null, locked: true })} />,
    );
    expect(screen.getByText(/vault is locked/i)).toBeInTheDocument();
    expect(screen.queryByText('At a glance')).not.toBeInTheDocument();
  });
});

describe('VariableInspector — usage section', () => {
  it('lists all consumers with the summary line', () => {
    render(
      <VariableInspector
        {...makeProps({ variable: SECRET_VAR, consumers: CONSUMERS })}
      />,
    );
    expect(screen.getByText('Referenced in stack')).toBeInTheDocument();
    expect(screen.getByText(/used by 2 sites/i)).toBeInTheDocument();
    expect(screen.getByText(/github · env\.API_KEY/)).toBeInTheDocument();
    expect(screen.getByText(/gateway · auth\.token/)).toBeInTheDocument();
  });

  it('navigates on consumer click', () => {
    const props = makeProps({ variable: SECRET_VAR, consumers: CONSUMERS });
    render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /go to github/i }));
    expect(props.onConsumerClick).toHaveBeenCalledWith(CONSUMERS[0]);
  });

  it('shows the hedged orphan callout with a delete shortcut', () => {
    const props = makeProps({ variable: SECRET_VAR, consumers: [] });
    render(<VariableInspector {...props} />);
    expect(screen.getByText(/not referenced by/i)).toBeInTheDocument();
    expect(screen.getByText(/secrets\.sets/)).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole('button', { name: /delete this variable/i }),
    );
    expect(props.onDelete).toHaveBeenCalledWith('API_KEY');
  });
});

describe('VariableInspector — value section', () => {
  it('masks a secret with a fixed-length mask', () => {
    render(<VariableInspector {...makeProps({ variable: SECRET_VAR })} />);
    expect(screen.getByLabelText('Value hidden')).toHaveTextContent(
      '••••••••••',
    );
  });

  it('copies the value without revealing it on screen', async () => {
    const props = makeProps({ variable: SECRET_VAR });
    render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /copy value/i }));
    await flush();
    expect(props.getValue).toHaveBeenCalledWith('API_KEY');
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('s3cret-value');
    // The mask never leaves the screen.
    expect(screen.getByLabelText('Value hidden')).toBeInTheDocument();
    expect(screen.queryByText('s3cret-value')).not.toBeInTheDocument();
  });

  it('reveals a secret and auto-hides it after 10 seconds', async () => {
    vi.useFakeTimers();
    render(<VariableInspector {...makeProps({ variable: SECRET_VAR })} />);
    fireEvent.click(screen.getByRole('button', { name: /^reveal$/i }));
    await flush();
    expect(screen.getByText('s3cret-value')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.queryByText('s3cret-value')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Value hidden')).toBeInTheDocument();
  });

  it('drops an in-flight reveal when the selection moves away', async () => {
    let resolveValue: (v: { value: string }) => void = () => {};
    const getValue = vi.fn().mockImplementation(
      () =>
        new Promise<{ value: string }>((res) => {
          resolveValue = res;
        }),
    );
    const props = makeProps({ variable: SECRET_VAR, getValue });
    const { rerender } = render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /^reveal$/i }));

    // Switch away while the fetch is in flight, then let it resolve.
    rerender(<VariableInspector {...props} variable={PLAIN_VAR} />);
    await act(async () => {
      resolveValue({ value: 's3cret-value' });
      await Promise.resolve();
    });

    // Back on the secret: the late resolution must not have unmasked it.
    rerender(<VariableInspector {...props} variable={SECRET_VAR} />);
    expect(screen.queryByText('s3cret-value')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Value hidden')).toBeInTheDocument();
  });

  it('shows a plaintext value without a reveal step', async () => {
    render(
      <VariableInspector
        {...makeProps({
          variable: PLAIN_VAR,
          getValue: vi.fn().mockResolvedValue({ value: 'v1.2.3' }),
        })}
      />,
    );
    await flush();
    expect(screen.getByText('v1.2.3')).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /^reveal$/i }),
    ).not.toBeInTheDocument();
  });
});

describe('VariableInspector — edit mode', () => {
  it('saves an edited value through onUpdate and exits edit mode', async () => {
    const props = makeProps({ variable: SECRET_VAR });
    render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /edit value/i }));

    const input = screen.getByPlaceholderText('secret value');
    fireEvent.change(input, { target: { value: 'next-value' } });
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await flush();

    expect(props.onUpdate).toHaveBeenCalledWith('API_KEY', {
      value: 'next-value',
      type: 'string',
      isSecret: true,
    });
    expect(
      screen.queryByPlaceholderText('secret value'),
    ).not.toBeInTheDocument();
  });

  it('cancels editing without saving', () => {
    const props = makeProps({ variable: SECRET_VAR });
    render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /edit value/i }));
    fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }));
    expect(props.onUpdate).not.toHaveBeenCalled();
    expect(
      screen.queryByPlaceholderText('secret value'),
    ).not.toBeInTheDocument();
  });

  it('cancels editing on Escape without clearing the selection', () => {
    const props = makeProps({ variable: SECRET_VAR });
    render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /edit value/i }));
    fireEvent.keyDown(screen.getByPlaceholderText('secret value'), {
      key: 'Escape',
    });
    expect(
      screen.queryByPlaceholderText('secret value'),
    ).not.toBeInTheDocument();
    expect(props.onClose).not.toHaveBeenCalled();
    // Still on the detail view, not the overview.
    expect(screen.getByRole('heading', { name: 'API_KEY' })).toBeInTheDocument();
  });
});

describe('VariableInspector — properties and actions', () => {
  it('reassigns the set from the move select', () => {
    const props = makeProps({ variable: PLAIN_VAR });
    render(<VariableInspector {...props} />);
    fireEvent.change(screen.getByLabelText('Move to set'), {
      target: { value: 'prod' },
    });
    expect(props.onAssignSet).toHaveBeenCalledWith('IMAGE_TAG', 'prod');
  });

  it('rotates a string secret behind a confirmation', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const props = makeProps({ variable: SECRET_VAR, onUpdate });
    render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /rotate secret/i }));
    fireEvent.click(await screen.findByRole('button', { name: /^rotate$/i }));
    await flush();

    expect(onUpdate).toHaveBeenCalledTimes(1);
    const [key, input] = onUpdate.mock.calls[0] as [
      string,
      { value: string; type: string; isSecret: boolean },
    ];
    expect(key).toBe('API_KEY');
    expect(input.type).toBe('string');
    expect(input.isSecret).toBe(true);
    expect(input.value).toHaveLength(24);
  });

  it('offers no rotate action for plaintext variables', async () => {
    render(<VariableInspector {...makeProps({ variable: PLAIN_VAR })} />);
    await flush();
    expect(
      screen.queryByRole('button', { name: /rotate secret/i }),
    ).not.toBeInTheDocument();
  });

  it('re-masks and resets edit state when the selection changes', async () => {
    const props = makeProps({ variable: SECRET_VAR });
    const { rerender } = render(<VariableInspector {...props} />);
    fireEvent.click(screen.getByRole('button', { name: /^reveal$/i }));
    await flush();
    expect(screen.getByText('s3cret-value')).toBeInTheDocument();

    rerender(
      <VariableInspector
        {...props}
        variable={PLAIN_VAR}
        getValue={vi.fn().mockResolvedValue({ value: 'v1.2.3' })}
      />,
    );
    await flush();
    rerender(<VariableInspector {...props} />);
    // Back on the secret: it starts masked again.
    expect(screen.queryByText('s3cret-value')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Value hidden')).toBeInTheDocument();
  });
});
