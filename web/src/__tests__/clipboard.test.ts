import { afterEach, describe, expect, it, vi } from 'vitest';
import { copyTextToClipboard } from '../lib/clipboard';

// jsdom has no document.execCommand, so each case installs the legacy path
// it exercises and removes it afterwards.
function setExecCommand(impl: ((command: string) => boolean) | undefined) {
  Object.defineProperty(document, 'execCommand', { value: impl, configurable: true, writable: true });
}

function rejectAsyncClipboard() {
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
}

afterEach(() => {
  setExecCommand(undefined);
  Object.assign(navigator, { clipboard: undefined });
});

describe('copyTextToClipboard', () => {
  it('resolves through the async clipboard API without touching the legacy path', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const execCommand = vi.fn(() => true);
    setExecCommand(execCommand);
    await expect(copyTextToClipboard('value')).resolves.toBeUndefined();
    expect(writeText).toHaveBeenCalledWith('value');
    expect(execCommand).not.toHaveBeenCalled();
  });

  it('resolves when the legacy copy command succeeds', async () => {
    rejectAsyncClipboard();
    let copied = '';
    setExecCommand((command) => {
      copied = document.querySelector('textarea')?.value ?? '';
      return command === 'copy';
    });
    await expect(copyTextToClipboard('legacy value')).resolves.toBeUndefined();
    expect(copied).toBe('legacy value');
    expect(document.querySelector('textarea')).toBeNull();
  });

  it('rejects when the legacy copy command returns false', async () => {
    rejectAsyncClipboard();
    setExecCommand(() => false);
    await expect(copyTextToClipboard('value')).rejects.toThrow();
    expect(document.querySelector('textarea')).toBeNull();
  });

  it('rejects when the legacy copy command throws', async () => {
    rejectAsyncClipboard();
    setExecCommand(() => {
      throw new Error('blocked');
    });
    await expect(copyTextToClipboard('value')).rejects.toThrow('blocked');
    expect(document.querySelector('textarea')).toBeNull();
  });

  it('rejects when the legacy copy command is missing', async () => {
    rejectAsyncClipboard();
    setExecCommand(undefined);
    await expect(copyTextToClipboard('value')).rejects.toThrow();
    expect(document.querySelector('textarea')).toBeNull();
  });

  it('falls back when the async clipboard API is absent', async () => {
    Object.assign(navigator, { clipboard: undefined });
    setExecCommand(() => true);
    await expect(copyTextToClipboard('value')).resolves.toBeUndefined();
  });
});
