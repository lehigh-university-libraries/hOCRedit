import {
  annotationBBox,
  annotationText,
  isLineAnnotation,
  isWordAnnotation,
} from '../utils/iiif';
import type {
  EditOperationCounts,
  EditOperationKind,
  IIIFAnnotation,
  IIIFAnnotationPage,
  ImageBBox,
  PageEditMetrics,
} from '../types/scribe';

export type { EditOperationCounts, EditOperationKind, PageEditMetrics };

// Edit metrics are derived from a base page and a draft page. They are a
// browser-side summary shown after a save and forwarded to the shell; the
// committed correction score itself is computed by the server against the
// model baseline when the page is saved.

const MAX_DISTANCE_TEXT_LENGTH = 2_000;

function itemsById(page: IIIFAnnotationPage | null | undefined): Map<string, IIIFAnnotation> {
  const map = new Map<string, IIIFAnnotation>();
  for (const item of Array.isArray(page?.items) ? page.items : []) {
    if (typeof item?.id === 'string' && item.id) map.set(item.id, item);
  }
  return map;
}

function sameBBox(left: ImageBBox, right: ImageBBox): boolean {
  return left.x === right.x && left.y === right.y && left.w === right.w && left.h === right.h;
}

/** Exact Unicode distance, or null when the remaining diff exceeds the UI budget. */
export function characterDistance(left: string, right: string): number | null {
  const leftPoints = Array.from(left);
  const rightPoints = Array.from(right);
  let start = 0;
  let leftEnd = leftPoints.length;
  let rightEnd = rightPoints.length;
  while (start < leftEnd && start < rightEnd && leftPoints[start] === rightPoints[start]) start += 1;
  while (leftEnd > start && rightEnd > start && leftPoints[leftEnd - 1] === rightPoints[rightEnd - 1]) {
    leftEnd -= 1;
    rightEnd -= 1;
  }
  const a = leftPoints.slice(start, leftEnd);
  const b = rightPoints.slice(start, rightEnd);
  if (a.length === 0) return b.length;
  if (b.length === 0) return a.length;
  if (a.length > MAX_DISTANCE_TEXT_LENGTH || b.length > MAX_DISTANCE_TEXT_LENGTH) return null;
  let previous = Array.from({ length: b.length + 1 }, (_, index) => index);
  for (let i = 1; i <= a.length; i += 1) {
    const current = [i];
    for (let j = 1; j <= b.length; j += 1) {
      const substitution = previous[j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1);
      current[j] = Math.min(previous[j] + 1, current[j - 1] + 1, substitution);
    }
    previous = current;
  }
  return previous[b.length];
}

export function emptyEditMetrics(): PageEditMetrics {
  return {
    boxesMoved: 0,
    boxesResized: 0,
    changedAnnotationIds: [],
    characterDistance: 0,
    linesAdded: 0,
    linesDeleted: 0,
    linesRetyped: 0,
    wordsAdded: 0,
    wordsDeleted: 0,
    wordsRetyped: 0,
  };
}

export function editMetricsAreEmpty(metrics: PageEditMetrics | null | undefined): boolean {
  return !metrics || metrics.changedAnnotationIds.length === 0;
}

/**
 * Compares two pages by canonical annotation ID. Text and geometry changes
 * are counted independently, so one word that was both retyped and nudged
 * contributes to both counts but appears once in `changedAnnotationIds`.
 */
export function pageEditMetrics(
  basePage: IIIFAnnotationPage | null | undefined,
  draftPage: IIIFAnnotationPage | null | undefined,
): PageEditMetrics {
  const metrics = emptyEditMetrics();
  const base = itemsById(basePage);
  const draft = itemsById(draftPage);
  const ids = new Set([...base.keys(), ...draft.keys()]);

  for (const id of ids) {
    const before = base.get(id) || null;
    const after = draft.get(id) || null;
    const subject = after || before;
    const isWord = isWordAnnotation(subject);
    const isLine = !isWord && isLineAnnotation(subject);
    if (!before && after) {
      if (isWord) metrics.wordsAdded += 1;
      else if (isLine) metrics.linesAdded += 1;
      metrics.changedAnnotationIds.push(id);
      continue;
    }
    if (before && !after) {
      if (isWord) metrics.wordsDeleted += 1;
      else if (isLine) metrics.linesDeleted += 1;
      metrics.changedAnnotationIds.push(id);
      continue;
    }
    if (!before || !after) continue;

    let changed = false;
    const beforeText = annotationText(before);
    const afterText = annotationText(after);
    if (beforeText !== afterText) {
      changed = true;
      if (isWord) metrics.wordsRetyped += 1;
      else if (isLine) {
        metrics.linesRetyped += 1;
        const distance = characterDistance(beforeText, afterText);
        metrics.characterDistance = metrics.characterDistance === null || distance === null
          ? null : metrics.characterDistance + distance;
      }
    }
    const beforeBox = annotationBBox(before);
    const afterBox = annotationBBox(after);
    if (!sameBBox(beforeBox, afterBox)) {
      changed = true;
      if (beforeBox.w === afterBox.w && beforeBox.h === afterBox.h) metrics.boxesMoved += 1;
      else metrics.boxesResized += 1;
    }
    if (changed) metrics.changedAnnotationIds.push(id);
  }
  metrics.changedAnnotationIds.sort();
  return metrics;
}

function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`;
}

/** Short human summary such as "2 words retyped, 1 box moved". */
export function formatEditMetrics(metrics: PageEditMetrics | null | undefined): string {
  if (!metrics || editMetricsAreEmpty(metrics)) return 'no changes';
  const parts: string[] = [];
  if (metrics.wordsRetyped) parts.push(`${plural(metrics.wordsRetyped, 'word')} retyped`);
  if (metrics.linesRetyped) parts.push(`${plural(metrics.linesRetyped, 'line')} retyped`);
  if (metrics.boxesMoved) parts.push(`${plural(metrics.boxesMoved, 'box', 'boxes')} moved`);
  if (metrics.boxesResized) parts.push(`${plural(metrics.boxesResized, 'box', 'boxes')} resized`);
  if (metrics.linesAdded) parts.push(`${plural(metrics.linesAdded, 'line')} added`);
  if (metrics.wordsAdded) parts.push(`${plural(metrics.wordsAdded, 'word')} added`);
  if (metrics.linesDeleted) parts.push(`${plural(metrics.linesDeleted, 'line')} deleted`);
  if (metrics.wordsDeleted) parts.push(`${plural(metrics.wordsDeleted, 'word')} deleted`);
  return parts.join(', ');
}

export function recordEditOperation(
  counts: EditOperationCounts,
  kind: EditOperationKind,
): EditOperationCounts {
  return { ...counts, [kind]: (counts[kind] || 0) + 1 };
}

export function totalEditOperations(counts: EditOperationCounts | null | undefined): number {
  return Object.values(counts || {}).reduce((sum, value) => sum + (value || 0), 0);
}
