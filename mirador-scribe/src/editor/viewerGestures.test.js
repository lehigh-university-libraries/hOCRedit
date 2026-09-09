// @vitest-environment happy-dom

import { describe, expect, it, vi } from 'vitest';
import {
  HIT_TARGET_ATTRIBUTE,
  INTERACTIVE_ATTRIBUTE,
  installViewerGestureGuard,
  isClickGesture,
} from './viewerGestures';

function fakeViewer() {
  const handlers = new Map();
  const tracker = { preProcessEventHandler: null };
  return {
    addHandler: vi.fn((name, handler) => handlers.set(name, handler)),
    handlers,
    innerTracker: tracker,
    removeHandler: vi.fn((name) => handlers.delete(name)),
  };
}

describe('viewer gesture guard', () => {
  it('opts interactive editor controls out of viewer gestures while keeping browser defaults', () => {
    const viewer = fakeViewer();
    installViewerGestureGuard(viewer);
    const input = document.createElement('input');
    const panel = document.createElement('div');
    panel.setAttribute(INTERACTIVE_ATTRIBUTE, 'true');
    panel.appendChild(input);
    document.body.appendChild(panel);

    const info = { originalEvent: { target: input }, preventDefault: true, preventGesture: false };
    viewer.innerTracker.preProcessEventHandler(info);
    expect(info).toMatchObject({ preventDefault: false, preventGesture: true });

    const plain = { originalEvent: { target: document.body }, preventDefault: true, preventGesture: false };
    viewer.innerTracker.preProcessEventHandler(plain);
    expect(plain).toMatchObject({ preventDefault: true, preventGesture: false });
    panel.remove();
  });

  it('keeps drags through hit targets pannable but stops a quick click from zooming', () => {
    const viewer = fakeViewer();
    installViewerGestureGuard(viewer);
    const hit = document.createElement('button');
    hit.setAttribute(HIT_TARGET_ATTRIBUTE, 'word');
    document.body.appendChild(hit);

    const pressInfo = { originalEvent: { target: hit }, preventDefault: true, preventGesture: false };
    viewer.innerTracker.preProcessEventHandler(pressInfo);
    expect(pressInfo.preventGesture).toBe(false);

    viewer.handlers.get('canvas-press')({ originalEvent: { target: hit } });
    const click = { originalEvent: { target: viewer }, preventDefaultAction: false };
    viewer.handlers.get('canvas-click')(click);
    expect(click.preventDefaultAction).toBe(true);

    const elsewhere = { originalEvent: { target: document.body }, preventDefaultAction: false };
    viewer.handlers.get('canvas-press')({ originalEvent: { target: document.body } });
    viewer.handlers.get('canvas-click')(elsewhere);
    expect(elsewhere.preventDefaultAction).toBe(false);
    hit.remove();
  });

  it('chains and restores a pre-existing pre-processor on uninstall', () => {
    const viewer = fakeViewer();
    const previous = vi.fn();
    viewer.innerTracker.preProcessEventHandler = previous;
    const uninstall = installViewerGestureGuard(viewer);
    viewer.innerTracker.preProcessEventHandler({ originalEvent: { target: document.body } });
    expect(previous).toHaveBeenCalledOnce();
    uninstall();
    expect(viewer.innerTracker.preProcessEventHandler).toBe(previous);
    expect(viewer.handlers.size).toBe(0);

    const removeAgain = installViewerGestureGuard(viewer);
    const replacement = vi.fn();
    viewer.innerTracker.preProcessEventHandler = replacement;
    removeAgain();
    expect(viewer.innerTracker.preProcessEventHandler).toBe(replacement);
  });

  it('is a no-op for viewers without an inner tracker', () => {
    expect(() => installViewerGestureGuard(null)()).not.toThrow();
    expect(() => installViewerGestureGuard({ addHandler: vi.fn(), removeHandler: vi.fn() })()).not.toThrow();
  });

  it('classifies a short press-release as a click and a longer one as a drag', () => {
    expect(isClickGesture({ x: 10, y: 10 }, { x: 13, y: 12 })).toBe(true);
    expect(isClickGesture({ x: 10, y: 10 }, { x: 30, y: 12 })).toBe(false);
  });
});
