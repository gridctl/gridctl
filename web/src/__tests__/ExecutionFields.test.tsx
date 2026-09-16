import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ExecutionFields } from '../components/wizard/steps/ExecutionFields';

describe('ExecutionFields', () => {
  it('suggests writable mounts instead of privilege for permission failures', () => {
    const onChange = vi.fn();
    render(<ExecutionFields data={{ name: 'echo', serverType: 'source', execution: { mode: 'hardened', uid: 10001, gid: 10001 } }} onChange={onChange} />);
    expect(screen.getByText(/declared scratch or data mounts/i)).toBeInTheDocument();
    expect(screen.getByText(/Do not use privileged mode, chmod 777/i)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Execution configuration'), { target: { value: '' } });
    expect(onChange).toHaveBeenCalled();
  });
});
