import { describe, expect, it } from 'vitest';
import {
  characterDistance,
  editMetricsAreEmpty,
  formatEditMetrics,
  pageEditMetrics,
  recordEditOperation,
  totalEditOperations,
} from './editMetrics';

const canvasId = 'https://example.test/canvas/1';

function annotation(id, text, textGranularity = 'line', bbox = '10,10,80,20') {
  return {
    body: [{ purpose: 'supplementing', type: 'TextualBody', value: text }],
    id,
    target: `${canvasId}#xywh=pixel:${bbox}`,
    textGranularity,
    type: 'Annotation',
  };
}

function page(items) {
  return { id: 'page-1', items, type: 'AnnotationPage' };
}

describe('pageEditMetrics', () => {
  it('reports no changes for an identical draft', () => {
    const base = page([annotation('line-1', 'alpha beta'), annotation('word-1', 'alpha', 'word')]);
    const metrics = pageEditMetrics(base, structuredClone(base));
    expect(editMetricsAreEmpty(metrics)).toBe(true);
    expect(formatEditMetrics(metrics)).toBe('no changes');
  });

  it('counts retyped words and lines with a character distance over lines only', () => {
    const base = page([
      annotation('line-1', 'alpha beta', 'line', '10,10,200,20'),
      annotation('word-1', 'alpha', 'word', '10,10,80,20'),
      annotation('word-2', 'beta', 'word', '100,10,80,20'),
    ]);
    const draft = page([
      annotation('line-1', 'alpha bets', 'line', '10,10,200,20'),
      annotation('word-1', 'alpha', 'word', '10,10,80,20'),
      annotation('word-2', 'bets', 'word', '100,10,80,20'),
    ]);
    const metrics = pageEditMetrics(base, draft);
    expect(metrics).toMatchObject({
      characterDistance: 1,
      linesRetyped: 1,
      wordsRetyped: 1,
      boxesMoved: 0,
      boxesResized: 0,
    });
    expect(metrics.changedAnnotationIds).toEqual(['line-1', 'word-2']);
    expect(formatEditMetrics(metrics)).toBe('1 word retyped, 1 line retyped');
  });

  it('distinguishes moved boxes from resized boxes', () => {
    const base = page([
      annotation('line-1', 'alpha', 'line', '10,10,200,20'),
      annotation('word-1', 'alpha', 'word', '10,10,80,20'),
    ]);
    const draft = page([
      annotation('line-1', 'alpha', 'line', '10,10,240,20'),
      annotation('word-1', 'alpha', 'word', '14,12,80,20'),
    ]);
    const metrics = pageEditMetrics(base, draft);
    expect(metrics).toMatchObject({ boxesMoved: 1, boxesResized: 1, linesRetyped: 0, wordsRetyped: 0 });
    expect(formatEditMetrics(metrics)).toBe('1 box moved, 1 box resized');
  });

  it('counts added and deleted lines and words separately', () => {
    const base = page([annotation('line-1', 'alpha'), annotation('word-1', 'alpha', 'word')]);
    const draft = page([
      annotation('line-2', 'beta'),
      annotation('word-2', 'beta', 'word'),
      annotation('word-3', 'gamma', 'word'),
    ]);
    const metrics = pageEditMetrics(base, draft);
    expect(metrics).toMatchObject({
      linesAdded: 1,
      linesDeleted: 1,
      wordsAdded: 2,
      wordsDeleted: 1,
    });
    expect(formatEditMetrics(metrics)).toBe('1 line added, 2 words added, 1 line deleted, 1 word deleted');
  });

  it('lists an annotation once even when both its text and box changed', () => {
    const base = page([annotation('word-1', 'alpha', 'word', '10,10,80,20')]);
    const draft = page([annotation('word-1', 'alpha!', 'word', '12,10,80,20')]);
    const metrics = pageEditMetrics(base, draft);
    expect(metrics.changedAnnotationIds).toEqual(['word-1']);
    expect(metrics).toMatchObject({ boxesMoved: 1, wordsRetyped: 1 });
  });

  it('tolerates a missing base page by treating every draft item as added', () => {
    const metrics = pageEditMetrics(null, page([annotation('line-1', 'alpha')]));
    expect(metrics.linesAdded).toBe(1);
  });
});

describe('characterDistance', () => {
  it('does not silently discard edits after the cost limit', () => {
    expect(characterDistance('a'.repeat(3000), `${'a'.repeat(3000)}b`)).toBe(1);
    expect(characterDistance('a'.repeat(3000), 'b'.repeat(3000))).toBeNull();
    expect(pageEditMetrics(
      page([annotation('line-1', 'a'.repeat(3000))]),
      page([annotation('line-1', 'b'.repeat(3000))]),
    ).characterDistance).toBeNull();
  });
  it('measures Unicode code points, not UTF-16 units', () => {
    expect(characterDistance('café', 'cafe')).toBe(1);
    expect(characterDistance('a😀b', 'ab')).toBe(1);
    expect(characterDistance('', 'abc')).toBe(3);
    expect(characterDistance('kitten', 'sitting')).toBe(3);
  });
});

describe('edit operation counters', () => {
  it('accumulates immutable per-kind counts', () => {
    let counts = {};
    counts = recordEditOperation(counts, 'text-edit');
    counts = recordEditOperation(counts, 'text-edit');
    counts = recordEditOperation(counts, 'box-move');
    expect(counts).toEqual({ 'box-move': 1, 'text-edit': 2 });
    expect(totalEditOperations(counts)).toBe(3);
    expect(totalEditOperations(null)).toBe(0);
  });
});
