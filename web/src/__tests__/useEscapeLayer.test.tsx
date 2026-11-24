import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { flushSync } from 'react-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { Modal } from '../components/ui/Modal';
import { SlideOver } from '../components/ui/SlideOver';
import { useEscapeLayer } from '../hooks/useEscapeLayer';

beforeEach(() => cleanup());
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function NestedModals({
  onOuter,
  onInner,
  innerInitiallyOpen = false,
}: {
  onOuter: () => void;
  onInner: () => void;
  innerInitiallyOpen?: boolean;
}) {
  const [inner, setInner] = useState(innerInitiallyOpen);
  return (
    <Modal isOpen onClose={onOuter} title="Outer">
      <button type="button" onClick={() => setInner(true)}>
        Open inner
      </button>
      <Modal
        isOpen={inner}
        onClose={() => {
          onInner();
          setInner(false);
        }}
        title="Inner"
      >
        <button type="button">Inner action</button>
        <input aria-label="Inner field" />
      </Modal>
    </Modal>
  );
}

describe('useEscapeLayer with nested modals', () => {
  it('closes only the inner modal, then the outer on the next Escape', () => {
    const onOuter = vi.fn();
    const onInner = vi.fn();
    render(<NestedModals onOuter={onOuter} onInner={onInner} />);
    fireEvent.click(screen.getByRole('button', { name: 'Open inner' }));

    fireEvent.keyDown(screen.getByRole('button', { name: 'Inner action' }), { key: 'Escape' });
    expect(onInner).toHaveBeenCalledTimes(1);
    expect(onOuter).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog', { name: 'Inner' })).not.toBeInTheDocument();

    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(onOuter).toHaveBeenCalledTimes(1);
    expect(onInner).toHaveBeenCalledTimes(1);
  });

  it('ranks the inner modal on top when both open in the same commit', () => {
    const onOuter = vi.fn();
    const onInner = vi.fn();
    render(<NestedModals onOuter={onOuter} onInner={onInner} innerInitiallyOpen />);

    fireEvent.keyDown(screen.getByRole('button', { name: 'Inner action' }), { key: 'Escape' });
    expect(onInner).toHaveBeenCalledTimes(1);
    expect(onOuter).not.toHaveBeenCalled();
  });

  it('blurs a field in the top modal without closing anything', () => {
    const onOuter = vi.fn();
    const onInner = vi.fn();
    render(<NestedModals onOuter={onOuter} onInner={onInner} innerInitiallyOpen />);
    const field = screen.getByLabelText('Inner field');
    field.focus();
    expect(field).toHaveFocus();

    fireEvent.keyDown(field, { key: 'Escape' });
    expect(field).not.toHaveFocus();
    expect(onInner).not.toHaveBeenCalled();
    expect(onOuter).not.toHaveBeenCalled();
  });
});

describe('useEscapeLayer with a SlideOver and a Modal', () => {
  function Layers({
    modalFirst,
    onSlide,
    onModal,
  }: {
    modalFirst: boolean;
    onSlide: () => void;
    onModal: () => void;
  }) {
    const slide = (
      <SlideOver key="slide" isOpen onClose={onSlide} title="Slide">
        <button type="button">Slide action</button>
      </SlideOver>
    );
    const modal = (
      <Modal key="modal" isOpen onClose={onModal} title="Gate">
        <button type="button">Modal action</button>
      </Modal>
    );
    return <>{modalFirst ? [modal, slide] : [slide, modal]}</>;
  }

  it('closes the modal opened after the SlideOver', () => {
    const onSlide = vi.fn();
    const onModal = vi.fn();
    const { rerender } = render(
      <SlideOver isOpen onClose={onSlide} title="Slide">
        <button type="button">Slide action</button>
      </SlideOver>,
    );
    rerender(<Layers modalFirst={false} onSlide={onSlide} onModal={onModal} />);
    fireEvent.keyDown(screen.getByRole('button', { name: 'Modal action' }), { key: 'Escape' });
    expect(onModal).toHaveBeenCalledTimes(1);
    expect(onSlide).not.toHaveBeenCalled();
  });

  it('still closes the modal when the SlideOver opened after it', () => {
    const onSlide = vi.fn();
    const onModal = vi.fn();
    const { rerender } = render(
      <Modal isOpen onClose={onModal} title="Gate">
        <button type="button">Modal action</button>
      </Modal>,
    );
    rerender(<Layers modalFirst onSlide={onSlide} onModal={onModal} />);
    fireEvent.keyDown(screen.getByRole('button', { name: 'Slide action' }), { key: 'Escape' });
    expect(onModal).toHaveBeenCalledTimes(1);
    expect(onSlide).not.toHaveBeenCalled();
  });
});

function RawLayer({ onEscape, modal = true }: { onEscape: () => void; modal?: boolean }) {
  useEscapeLayer({ active: true, onEscape, modal });
  return null;
}

describe('useEscapeLayer dispatch', () => {
  it('does not promote the layer beneath when the top leaves during its own onEscape', () => {
    const lower = vi.fn();
    const top = vi.fn();
    function Harness() {
      const [topOpen, setTopOpen] = useState(true);
      return (
        <>
          <RawLayer onEscape={lower} />
          {topOpen && (
            <RawLayer
              onEscape={() => {
                top();
                // Stands in for the browser's microtask flush: the top layer
                // unmounts and unregisters before the dispatch returns.
                flushSync(() => setTopOpen(false));
              }}
            />
          )}
        </>
      );
    }
    render(<Harness />);

    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(top).toHaveBeenCalledTimes(1);
    expect(lower).not.toHaveBeenCalled();

    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(lower).toHaveBeenCalledTimes(1);
    expect(top).toHaveBeenCalledTimes(1);
  });

  it('holds one document keydown listener for every layer and removes it after the last', () => {
    const add = vi.spyOn(document, 'addEventListener');
    const remove = vi.spyOn(document, 'removeEventListener');
    function Harness({ count }: { count: number }) {
      return (
        <>
          {Array.from({ length: count }, (_, i) => (
            <RawLayer key={i} onEscape={() => {}} modal={i % 2 === 0} />
          ))}
        </>
      );
    }
    const { rerender } = render(<Harness count={3} />);
    const added = add.mock.calls.filter(([type]) => type === 'keydown');
    expect(added).toHaveLength(1);
    const listener = added[0][1];

    rerender(<Harness count={1} />);
    expect(remove.mock.calls.filter(([type]) => type === 'keydown')).toHaveLength(0);

    rerender(<Harness count={0} />);
    const removed = remove.mock.calls.filter(([type]) => type === 'keydown');
    expect(removed).toHaveLength(1);
    expect(removed[0][1]).toBe(listener);
  });

  it('keeps a consumed Escape from reaching window', () => {
    const onEscape = vi.fn();
    const onWindow = vi.fn();
    window.addEventListener('keydown', onWindow);
    render(
      <Modal isOpen onClose={onEscape} title="Prefs">
        <button type="button">Action</button>
      </Modal>,
    );
    fireEvent.keyDown(screen.getByRole('button', { name: 'Action' }), { key: 'Escape' });
    expect(onEscape).toHaveBeenCalledTimes(1);
    expect(onWindow).not.toHaveBeenCalled();

    // Other keys are not the stack's to stop.
    fireEvent.keyDown(screen.getByRole('button', { name: 'Action' }), { key: 'a' });
    expect(onWindow).toHaveBeenCalledTimes(1);
    window.removeEventListener('keydown', onWindow);
  });

  it('reads the latest onEscape without re-registering', () => {
    const first = vi.fn();
    const second = vi.fn();
    const add = vi.spyOn(document, 'addEventListener');
    const { rerender } = render(<RawLayer onEscape={first} />);
    rerender(<RawLayer onEscape={second} />);
    expect(add.mock.calls.filter(([type]) => type === 'keydown')).toHaveLength(1);
    act(() => {
      fireEvent.keyDown(document.body, { key: 'Escape' });
    });
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });
});
