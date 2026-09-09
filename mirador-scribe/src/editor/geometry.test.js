// @vitest-environment happy-dom

import { describe, expect, it } from 'vitest';

import {
  clientPointToImage,
  imagePointToViewerElement,
  imageBBoxContainsCenter,
  initialLineBBoxForViewport,
  viewerElementPointToImage,
  wordBBoxBesideSelection,
} from './geometry';

describe('OpenSeadragon geometry boundaries', () => {
  it('uses points owned by the mounted viewer across package boundaries', () => {
    class ViewerPoint {
      constructor(x, y) {
        this.x = x;
        this.y = y;
      }
    }
    const isViewerPoint = (point) => point instanceof ViewerPoint;
    const image = {
      imageToViewerElementCoordinates: (point) => (
        isViewerPoint(point) ? new ViewerPoint(point.x / 2, point.y / 3) : new ViewerPoint(NaN, NaN)
      ),
      viewerElementToImageCoordinates: (point) => (
        isViewerPoint(point) ? new ViewerPoint(point.x * 2, point.y * 3) : new ViewerPoint(0, 0)
      ),
      windowToImageCoordinates: (point) => (
        isViewerPoint(point) ? new ViewerPoint(point.x * 4, point.y * 5) : new ViewerPoint(0, 0)
      ),
    };
    const viewer = {
      viewport: { getCenter: () => new ViewerPoint(0, 0) },
      world: { getItemAt: () => image, getItemCount: () => 1 },
    };

    expect(viewerElementPointToImage(viewer, { x: 4, y: 5 })).toMatchObject({ x: 8, y: 15 });
    expect(imagePointToViewerElement(viewer, 8, 15)).toMatchObject({ x: 4, y: 5 });
    expect(clientPointToImage(viewer, 7, 11, { x: 0, y: 0 })).toMatchObject({ x: 28, y: 55 });
  });

  it('centers a keyboard-created line inside the visible image intersection', () => {
    expect(initialLineBBoxForViewport(
      { x: -100, y: 100, w: 1_000, h: 500 },
      { width: 800, height: 600 },
    )).toEqual({ x: 112, y: 320, w: 576, h: 60 });
    expect(initialLineBBoxForViewport(
      { x: 900, y: 100, w: 20, h: 20 },
      { width: 800, height: 600 },
    )).toBeNull();
  });

  it('keeps an adjacent draft word inside its containing line at the image edge', () => {
    expect(wordBBoxBesideSelection(
      { x: 80, y: 20, w: 10, h: 12 },
      { x: 10, y: 18, w: 100, h: 16 },
    )).toEqual({ x: 90, y: 20, w: 10, h: 12 });
    expect(wordBBoxBesideSelection(
      { x: 100, y: 20, w: 10, h: 12 },
      { x: 10, y: 18, w: 100, h: 16 },
    )).toEqual({ x: 109, y: 20, w: 1, h: 12 });
  });

  it('requires the integer word center to be inside both line axes', () => {
    const lineBBox = { x: 10, y: 10, w: 100, h: 20 };
    expect(imageBBoxContainsCenter(lineBBox, { x: 100, y: 20, w: 20, h: 1 })).toBe(true);
    expect(imageBBoxContainsCenter(lineBBox, { x: 110, y: 20, w: 2, h: 1 })).toBe(false);
    expect(imageBBoxContainsCenter(lineBBox, { x: 20, y: 30, w: 1, h: 2 })).toBe(false);
  });
});

describe('box move, resize, and containment', () => {
  it('resizes from each corner while anchoring the opposite corner', async () => {
    const { resizeBBoxFromHandle } = await import('./geometry');
    const box = { h: 20, w: 100, x: 10, y: 10 };
    expect(resizeBBoxFromHandle(box, 'se', 5, 3)).toEqual({ h: 23, w: 105, x: 10, y: 10 });
    expect(resizeBBoxFromHandle(box, 'nw', 5, 3)).toEqual({ h: 17, w: 95, x: 15, y: 13 });
    expect(resizeBBoxFromHandle(box, 'ne', -5, -3)).toEqual({ h: 23, w: 95, x: 10, y: 7 });
    expect(resizeBBoxFromHandle(box, 'sw', -5, 3)).toEqual({ h: 23, w: 105, x: 5, y: 10 });
  });

  it('never inverts a box when a corner is dragged past the opposite edge', async () => {
    const { resizeBBoxFromHandle } = await import('./geometry');
    const box = { h: 20, w: 100, x: 10, y: 10 };
    const inverted = resizeBBoxFromHandle(box, 'se', -150, -40);
    expect(inverted.w).toBeGreaterThanOrEqual(1);
    expect(inverted.h).toBeGreaterThanOrEqual(1);
    expect(inverted.x).toBeGreaterThanOrEqual(0);
    expect(inverted.y).toBeGreaterThanOrEqual(0);
  });

  it('translates a box without changing its size and never past the origin', async () => {
    const { translateBBox } = await import('./geometry');
    expect(translateBBox({ h: 20, w: 100, x: 10, y: 10 }, 7, -4)).toEqual({ h: 20, w: 100, x: 17, y: 6 });
    expect(translateBBox({ h: 20, w: 100, x: 10, y: 10 }, -50, -50)).toEqual({ h: 20, w: 100, x: 0, y: 0 });
  });

  it('keeps a word inside its owning line when moved or grown past an edge', async () => {
    const { clampBBoxWithin, translateBBox } = await import('./geometry');
    const line = { h: 30, w: 400, x: 100, y: 200 };
    const word = { h: 24, w: 60, x: 420, y: 203 };
    expect(clampBBoxWithin(translateBBox(word, 100, 0), line)).toEqual({ h: 24, w: 60, x: 440, y: 203 });
    expect(clampBBoxWithin(translateBBox(word, -400, -50), line)).toEqual({ h: 24, w: 60, x: 100, y: 200 });
    expect(clampBBoxWithin({ h: 80, w: 900, x: 0, y: 0 }, line)).toEqual({ h: 30, w: 400, x: 100, y: 200 });
  });

  it('keeps a line inside the canonical image and fails closed without image dimensions', async () => {
    const { clampBBoxWithin, imageBoundsBBox } = await import('./geometry');
    const image = imageBoundsBBox({ height: 1000, width: 800 });
    expect(image).toEqual({ h: 1000, w: 800, x: 0, y: 0 });
    expect(clampBBoxWithin({ h: 40, w: 300, x: 700, y: 990 }, image)).toEqual({ h: 40, w: 300, x: 500, y: 960 });
    expect(imageBoundsBBox(null)).toBeNull();
    expect(imageBoundsBBox({ height: 0, width: 800 })).toBeNull();
    expect(imageBoundsBBox({ height: Number.NaN, width: 800 })).toBeNull();
  });

  it('reveals an off-screen box with the smallest pan and leaves a visible box alone', async () => {
    const { viewportOffsetToReveal } = await import('./geometry');
    const viewport = { h: 500, w: 800, x: 100, y: 100 };
    expect(viewportOffsetToReveal({ h: 20, w: 100, x: 200, y: 200 }, viewport)).toBeNull();
    expect(viewportOffsetToReveal({ h: 20, w: 100, x: 200, y: 700 }, viewport)).toEqual({ x: 0, y: 120 });
    expect(viewportOffsetToReveal({ h: 20, w: 100, x: 20, y: 40 }, viewport)).toEqual({ x: -80, y: -60 });
    expect(viewportOffsetToReveal({ h: 20, w: 100, x: 200, y: 590 }, viewport, 20)).toEqual({ x: 0, y: 30 });
    expect(viewportOffsetToReveal(
      { h: 20, w: 100, x: 0, y: 0 },
      { h: 500, w: 800, x: -100, y: -100 },
      20,
    )).toBeNull();
  });
});
