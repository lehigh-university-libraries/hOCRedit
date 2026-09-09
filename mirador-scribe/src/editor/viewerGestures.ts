import type OpenSeadragon from 'openseadragon';

/**
 * Elements carrying this attribute own their pointer interaction. The viewer
 * neither pans, zooms, nor click-zooms for gestures that start on them, and
 * the browser default (focus, caret placement, text selection) is preserved.
 */
export const INTERACTIVE_ATTRIBUTE = 'data-scribe-interactive';

/**
 * Elements carrying this attribute are click targets that must still let a
 * drag pan the image. Only quick clicks on them are kept from zooming.
 */
export const HIT_TARGET_ATTRIBUTE = 'data-scribe-hit';

interface GestureEventInfo {
  originalEvent: Event;
  preventDefault?: boolean;
  preventGesture?: boolean;
}

interface CanvasGestureEvent {
  originalEvent?: Event;
  preventDefaultAction?: boolean;
}

interface GestureTracker {
  preProcessEventHandler?: ((eventInfo: GestureEventInfo) => void) | null;
}

type GuardedViewer = OpenSeadragon.Viewer & { innerTracker?: GestureTracker };

function eventTargetElement(event: Event | undefined): Element | null {
  const target = event?.target;
  return target instanceof Element ? target : null;
}

export function isInteractiveTarget(target: Element | null): boolean {
  return Boolean(target?.closest(`[${INTERACTIVE_ATTRIBUTE}]`));
}

export function isHitTarget(target: Element | null): boolean {
  return Boolean(target?.closest(`[${HIT_TARGET_ATTRIBUTE}]`));
}

/**
 * Installs the gesture guard on a mounted viewer and returns its uninstaller.
 * The previous pre-processor is preserved and restored so an embedding shell
 * that installs its own guard is not silently overridden.
 */
export function installViewerGestureGuard(viewer: OpenSeadragon.Viewer | null | undefined): () => void {
  const guarded = viewer as GuardedViewer | null | undefined;
  const tracker = guarded?.innerTracker;
  if (!guarded || !tracker) return () => {};

  const previous = tracker.preProcessEventHandler || null;
  let pressedHitTarget = false;
  const preprocess = (eventInfo: GestureEventInfo) => {
    previous?.(eventInfo);
    const target = eventTargetElement(eventInfo.originalEvent);
    if (isInteractiveTarget(target)) {
      eventInfo.preventGesture = true;
      eventInfo.preventDefault = false;
    }
  };
  tracker.preProcessEventHandler = preprocess;

  const handlePress = (event: CanvasGestureEvent) => {
    pressedHitTarget = isHitTarget(eventTargetElement(event.originalEvent));
  };
  const handleClick = (event: CanvasGestureEvent) => {
    if (pressedHitTarget || isHitTarget(eventTargetElement(event.originalEvent))) {
      event.preventDefaultAction = true;
    }
    pressedHitTarget = false;
  };
  const handleRelease = () => {
    // A release without a click (a drag) ends the press; a click arrives
    // after release, so the flag is cleared there instead when it applies.
    window.setTimeout(() => { pressedHitTarget = false; }, 0);
  };
  guarded.addHandler('canvas-press', handlePress);
  guarded.addHandler('canvas-click', handleClick);
  guarded.addHandler('canvas-release', handleRelease);

  return () => {
    if (tracker.preProcessEventHandler === preprocess) tracker.preProcessEventHandler = previous;
    guarded.removeHandler('canvas-press', handlePress);
    guarded.removeHandler('canvas-click', handleClick);
    guarded.removeHandler('canvas-release', handleRelease);
  };
}

/** Distance in CSS pixels under which a press-release pair counts as a click. */
export const CLICK_DISTANCE_PX = 6;

export function isClickGesture(
  start: { x: number; y: number },
  end: { x: number; y: number },
  threshold = CLICK_DISTANCE_PX,
): boolean {
  return Math.hypot(end.x - start.x, end.y - start.y) <= threshold;
}
