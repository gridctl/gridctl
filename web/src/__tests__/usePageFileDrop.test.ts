import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { usePageFileDrop } from '../hooks/usePageFileDrop';

// jsdom doesn't construct real DragEvents with a DataTransfer, so synthesize a
// plain Event and attach a duck-typed dataTransfer.
function dragEvent(
  type: string,
  {
    types = ['Files'],
    files = [] as unknown[],
    relatedTarget = document.body as EventTarget | null,
  } = {},
) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.assign(event, { dataTransfer: { types, files }, relatedTarget });
  return event;
}

function dispatch(event: Event) {
  act(() => {
    window.dispatchEvent(event);
  });
}

describe('usePageFileDrop', () => {
  it('does not activate while disabled', () => {
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: false, onFiles: vi.fn() }),
    );
    dispatch(dragEvent('dragenter'));
    expect(result.current.isDragging).toBe(false);
  });

  it('activates on a file dragenter and deactivates on the matching dragleave', () => {
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: true, onFiles: vi.fn() }),
    );
    dispatch(dragEvent('dragenter'));
    expect(result.current.isDragging).toBe(true);
    dispatch(dragEvent('dragleave'));
    expect(result.current.isDragging).toBe(false);
  });

  it('stays inactive across nested dragenter/leave (depth counter)', () => {
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: true, onFiles: vi.fn() }),
    );
    dispatch(dragEvent('dragenter')); // depth 1
    dispatch(dragEvent('dragenter')); // depth 2 (crossed into a child)
    dispatch(dragEvent('dragleave')); // depth 1 — still dragging
    expect(result.current.isDragging).toBe(true);
    dispatch(dragEvent('dragleave')); // depth 0
    expect(result.current.isDragging).toBe(false);
  });

  it('ignores drags that carry no files', () => {
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: true, onFiles: vi.fn() }),
    );
    dispatch(dragEvent('dragenter', { types: ['text/plain'] }));
    expect(result.current.isDragging).toBe(false);
  });

  it('calls onFiles with the dropped files and resets dragging', () => {
    const onFiles = vi.fn();
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: true, onFiles }),
    );
    const files = [{ name: 'a.env' }];
    dispatch(dragEvent('dragenter'));
    dispatch(dragEvent('drop', { files }));
    expect(onFiles).toHaveBeenCalledTimes(1);
    expect(onFiles.mock.calls[0][0]).toBe(files);
    expect(result.current.isDragging).toBe(false);
  });

  it('does not fire onFiles for a fileless drop', () => {
    const onFiles = vi.fn();
    renderHook(() => usePageFileDrop({ enabled: true, onFiles }));
    dispatch(dragEvent('drop', { types: ['text/plain'] }));
    expect(onFiles).not.toHaveBeenCalled();
  });

  it('resets when the drag is cancelled (dragend)', () => {
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: true, onFiles: vi.fn() }),
    );
    dispatch(dragEvent('dragenter'));
    expect(result.current.isDragging).toBe(true);
    dispatch(new Event('dragend'));
    expect(result.current.isDragging).toBe(false);
  });

  it('resets when the cursor leaves the window (null relatedTarget)', () => {
    const { result } = renderHook(() =>
      usePageFileDrop({ enabled: true, onFiles: vi.fn() }),
    );
    dispatch(dragEvent('dragenter')); // depth 1
    dispatch(dragEvent('dragenter')); // depth 2
    dispatch(dragEvent('dragleave', { relatedTarget: null }));
    expect(result.current.isDragging).toBe(false);
  });
});
