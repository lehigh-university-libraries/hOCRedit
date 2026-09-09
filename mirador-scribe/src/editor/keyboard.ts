export type EditorKeyboardCommand =
  | 'save'
  | 'dismiss-overlay'
  | 'undo'
  | 'redo'
  | 'delete'
  | 'edit-overlay'
  | 'read-overlay'
  | 'transcript-overlay'
  | 'add-line'
  | 'add-word'
  | 'split-line'
  | 'join-lines'
  | 'join-words'
  | 'reprocess'
  | 'publish';

export function isEditableEventTarget(target: EventTarget | null | undefined): boolean {
  return target instanceof HTMLElement
    && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable);
}

/**
 * Resolves window-level editor shortcuts without stealing native text-editing
 * keys. Escape and Save are intentionally available from inside an input.
 * Plain letter shortcuts only fire when no text field owns focus.
 */
export function editorKeyboardCommand(
  event: Pick<KeyboardEvent, 'altKey' | 'ctrlKey' | 'key' | 'metaKey' | 'shiftKey' | 'target'>,
  editableTarget = isEditableEventTarget(event.target),
): EditorKeyboardCommand | null {
  const key = String(event.key || '');
  const lower = key.toLowerCase();
  const command = Boolean(event.metaKey || event.ctrlKey);
  if (command && lower === 's') return 'save';
  if (!command && !event.altKey && key === 'Escape') return 'dismiss-overlay';
  if (editableTarget) return null;
  if (command && lower === 'z') return event.shiftKey ? 'redo' : 'undo';
  if (command && (key === 'Backspace' || key === 'Delete')) return 'delete';
  if (!command && !event.altKey && key === 'Delete') return 'delete';
  if (!command && event.altKey && lower === 's') return 'split-line';
  if (!command && event.altKey && lower === 'l') return 'join-lines';
  if (!command && event.altKey && lower === 'w') return 'join-words';
  if (!command && event.altKey && lower === 'r') return 'reprocess';
  if (!command && event.altKey && lower === 'p') return 'publish';
  if (command || event.altKey) return null;
  if (lower === 'e') return 'edit-overlay';
  if (lower === 'r') return 'read-overlay';
  if (lower === 't') return 'transcript-overlay';
  if (lower === 'n') return 'add-line';
  if (lower === 'w') return 'add-word';
  return null;
}
