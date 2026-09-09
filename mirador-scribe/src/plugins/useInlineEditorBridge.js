import {
  findEditorRowByAnnotationId,
  isWordAnnotation,
  lineAnnotationForSelection,
  rowSelectionId,
  synchronizeLineTextFromWords,
  updateAnnotationBBox,
  updateAnnotationText,
  updateRowText,
  upsertAnnotationInPage,
  wordAnnotationIdForCaret,
} from '../utils/iiif';
import { editorBridgeEventDetail, useDocumentEvent } from './useDocumentEvent';

/** @typedef {import('../types/scribe').EditorRow} EditorRow */
/** @typedef {import('../types/scribe').EditOperationKind} EditOperationKind */
/** @typedef {import('../types/scribe').IIIFAnnotation} IIIFAnnotation */
/** @typedef {import('../types/scribe').IIIFAnnotationPage} IIIFAnnotationPage */
/** @typedef {import('../types/scribe').ScribeOverlayMode} OverlayMode */

/**
 * Routes inline-overlay editing events for exactly one Mirador window and
 * Canvas. All mutation paths apply the caller's current editability fence.
 *
 * @param {Object} options
 * @param {string} options.canvasId
 * @param {string} options.effectiveSelectedAnnotationId
 * @param {(canvasId?: string) => boolean} options.editingIsBlocked
 * @param {() => void | Promise<void>} options.handleSave
 * @param {IIIFAnnotationPage | null} options.localPage
 * @param {(page: IIIFAnnotationPage) => void} options.pushHistory
 * @param {(kind: EditOperationKind) => void} [options.recordOperation]
 * @param {IIIFAnnotation | null} options.selectedAnnotation
 * @param {(windowId: string, annotationId: string) => unknown} options.selectAnnotation
 * @param {IIIFAnnotationPage | null} options.serverPage
 * @param {(active: boolean) => void} options.setDrawMode
 * @param {(annotationId: string) => void} options.setFocusedWordAnnotationId
 * @param {(mode: OverlayMode) => void} options.setOverlayMode
 * @param {(message: string) => void} options.setStatusMessage
 * @param {EditorRow[]} options.visibleRows
 * @param {string} options.windowId
 */
export function useInlineEditorBridge({
  canvasId,
  effectiveSelectedAnnotationId,
  editingIsBlocked,
  handleSave,
  localPage,
  pushHistory,
  recordOperation = () => {},
  selectedAnnotation,
  selectAnnotation,
  serverPage,
  setDrawMode,
  setFocusedWordAnnotationId,
  setOverlayMode,
  setStatusMessage,
  visibleRows,
  windowId,
}) {
  /**
   * @param {string} text
   * @param {number | null | undefined} selectionStart
   * @param {string} [annotationId] Row to edit; defaults to the current selection.
   */
  function changeText(text, selectionStart, annotationId = '') {
    if (!localPage || editingIsBlocked()) return;
    const targetRow = annotationId
      ? findEditorRowByAnnotationId(localPage, annotationId)
      : findEditorRowByAnnotationId(localPage, effectiveSelectedAnnotationId)
        || findEditorRowByAnnotationId(localPage, selectedAnnotation?.id || '');
    if (!targetRow) return;

    if (targetRow.granularity === 'word') {
      const activeWordId = wordAnnotationIdForCaret(targetRow, text, selectionStart);
      setFocusedWordAnnotationId(activeWordId || targetRow.fields[0]?.id || '');
      const lineId = rowSelectionId(targetRow);
      if (lineId) selectAnnotation(windowId, lineId);
      pushHistory(updateRowText(localPage, targetRow, text));
    } else {
      setFocusedWordAnnotationId('');
      const targetId = rowSelectionId(targetRow);
      const targetAnnotation = (localPage.items || []).find((annotation) => annotation?.id === targetId);
      if (!targetAnnotation) return;
      if (annotationId && targetId && targetId !== effectiveSelectedAnnotationId) {
        selectAnnotation(windowId, targetId);
      }
      pushHistory(upsertAnnotationInPage(localPage, updateAnnotationText(targetAnnotation, text)));
    }
    recordOperation('text-edit');
    setStatusMessage('');
  }

  /** @param {string} annotationId @param {string} text */
  function changeWord(annotationId, text) {
    if (!localPage || editingIsBlocked()) return;
    const wordAnnotation = (localPage.items || []).find((annotation) => annotation?.id === annotationId);
    if (!wordAnnotation) return;
    const nextPage = upsertAnnotationInPage(localPage, updateAnnotationText(wordAnnotation, text));
    const changedWord = nextPage.items.find((annotation) => annotation?.id === annotationId);
    if (!changedWord) return;
    pushHistory(synchronizeLineTextFromWords(nextPage, changedWord));
    setFocusedWordAnnotationId(annotationId);
    selectAnnotation(windowId, annotationId);
    recordOperation('text-edit');
    setStatusMessage('');
  }

  useDocumentEvent('scribe:inline-change-text', (event) => {
    const detail = editorBridgeEventDetail(event);
    if (detail.windowId !== windowId || detail.canvasId !== canvasId) return;
    changeText(detail.text || '', detail.selectionStart, detail.annotationId || '');
  });

  useDocumentEvent('scribe:inline-change-word', (event) => {
    const detail = editorBridgeEventDetail(event);
    if (detail.windowId !== windowId || detail.canvasId !== canvasId || !detail.annotationId) return;
    changeWord(detail.annotationId, detail.text || '');
  });

  useDocumentEvent('scribe:inline-step-selection', (event) => {
    const detail = editorBridgeEventDetail(event);
    if (detail.windowId !== windowId || detail.canvasId !== canvasId) return;
    if (editingIsBlocked() || visibleRows.length === 0) return;
    const currentRowId = lineAnnotationForSelection(localPage, selectedAnnotation)?.id
      || effectiveSelectedAnnotationId;
    const currentIndex = visibleRows.findIndex((row) => (
      row.lead?.id === currentRowId
        || row.fields.some((annotation) => annotation.id === currentRowId)
    ));
    const direction = detail.direction === -1 ? -1 : 1;
    const nextIndex = ((currentIndex >= 0 ? currentIndex : 0) + direction + visibleRows.length)
      % visibleRows.length;
    const nextRow = visibleRows[nextIndex];
    const nextSelection = rowSelectionId(nextRow);
    setFocusedWordAnnotationId(nextRow?.granularity === 'word' ? (nextRow.fields[0]?.id || '') : '');
    if (nextSelection) selectAnnotation(windowId, nextSelection);
  });

  useDocumentEvent('scribe:inline-save', (event) => {
    const detail = editorBridgeEventDetail(event);
    if (detail.windowId !== windowId || detail.canvasId !== canvasId) return;
    void handleSave();
  });

  useDocumentEvent('scribe:select-annotation', (event) => {
    const detail = editorBridgeEventDetail(event);
    if (detail.windowId !== windowId || detail.canvasId !== canvasId) return;
    if (editingIsBlocked() || !detail.annotationId) return;
    const sourcePage = localPage || serverPage;
    const clickedAnnotation = (sourcePage?.items || [])
      .find((annotation) => annotation?.id === detail.annotationId) || null;
    setDrawMode(false);
    // A selection made from the image always lands in an editable mode. The
    // transcript pane keeps its own mode; every other origin opens the inline
    // editor so the clicked word can be changed immediately.
    setOverlayMode(detail.overlayMode === 'transcript' ? 'transcript' : 'edit');
    setFocusedWordAnnotationId(detail.focusAnnotationId
      || (clickedAnnotation && isWordAnnotation(clickedAnnotation) ? clickedAnnotation.id || '' : ''));
    selectAnnotation(windowId, detail.focusAnnotationId || detail.annotationId);
  });

  useDocumentEvent('scribe:resize-annotation', (event) => {
    const detail = editorBridgeEventDetail(event);
    if (detail.windowId !== windowId || detail.canvasId !== canvasId) return;
    if (editingIsBlocked()) return;
    const { annotationId, bbox } = detail;
    if (!annotationId || !bbox || !localPage) return;
    const annotation = (localPage.items || []).find((item) => item?.id === annotationId);
    if (!annotation) return;
    pushHistory(upsertAnnotationInPage(localPage, updateAnnotationBBox(annotation, bbox)));
    recordOperation(detail.operation === 'move' ? 'box-move' : 'box-resize');
  });
}
