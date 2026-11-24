import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useRef, useState, type RefObject } from 'react';
import { createPortal, flushSync } from 'react-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { Modal } from '../components/ui/Modal';
import { SlideOver } from '../components/ui/SlideOver';
import { SpecDiffModal } from '../components/spec/SpecDiffModal';
import { useEscapeLayer } from '../hooks/useEscapeLayer';
import { useSpecStore } from '../stores/useSpecStore';

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

describe('useEscapeLayer with a SlideOver and the SpecDiffModal', () => {
  beforeEach(() => {
    useSpecStore.setState({
      diffModalOpen: false,
      diffModalMode: 'apply',
      plan: null,
      planLoading: false,
      planError: null,
      spec: null,
    });
  });

  it('dismisses only the diff', () => {
    const onSlide = vi.fn();
    const onDismiss = vi.fn();
    render(
      <>
        <SlideOver isOpen onClose={onSlide} title="Slide">
          <input aria-label="Slide field" />
        </SlideOver>
        <SpecDiffModal onApply={() => undefined} onDismiss={onDismiss} />
      </>,
    );
    act(() => {
      useSpecStore.getState().openApplyPlan({
        hasChanges: true,
        items: [{ action: 'change', kind: 'mcp-server', name: 'github', details: ['image changed'] }],
        summary: '1 to change',
      });
    });
    expect(screen.getByRole('dialog', { name: /reload config/i })).toBeInTheDocument();

    fireEvent.keyDown(screen.getByLabelText('Slide field'), { key: 'Escape' });
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(onSlide).not.toHaveBeenCalled();
  });
});

function RawLayer({
  onEscape,
  modal = true,
  layerRef,
  closeFromFields,
}: {
  onEscape: () => void;
  modal?: boolean;
  layerRef?: RefObject<HTMLElement | null>;
  closeFromFields?: boolean;
}) {
  useEscapeLayer({ active: true, onEscape, modal, ref: layerRef, closeFromFields });
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

  it('closes from a field when closeFromFields is set', () => {
    const onEscape = vi.fn();
    render(
      <>
        <RawLayer onEscape={onEscape} closeFromFields />
        <input aria-label="Field" />
      </>,
    );
    const field = screen.getByLabelText('Field');
    field.focus();
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(onEscape).toHaveBeenCalledTimes(1);
    expect(field).toHaveFocus();
  });

  it('ranks a nested layer above its host when a portaled sibling registers between them', () => {
    const onHost = vi.fn();
    const onNested = vi.fn();
    const onPortaled = vi.fn();
    function Host() {
      const hostRef = useRef<HTMLDivElement>(null);
      const nestedRef = useRef<HTMLDivElement>(null);
      const portaledRef = useRef<HTMLDivElement>(null);
      // Effects run children first, so registration is nested, portaled, host.
      useEscapeLayer({ active: true, onEscape: onHost, modal: true, ref: hostRef });
      return (
        <div ref={hostRef}>
          <div ref={nestedRef} />
          <RawLayer onEscape={onNested} layerRef={nestedRef} />
          {createPortal(<div ref={portaledRef} />, document.body)}
          <RawLayer onEscape={onPortaled} layerRef={portaledRef} />
        </div>
      );
    }
    render(<Host />);
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(onNested).toHaveBeenCalledTimes(1);
    expect(onHost).not.toHaveBeenCalled();
    expect(onPortaled).not.toHaveBeenCalled();
  });
});
