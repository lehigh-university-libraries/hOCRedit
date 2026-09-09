// @vitest-environment happy-dom

import { describe, expect, it } from 'vitest';

import { editorKeyboardCommand, isEditableEventTarget } from './keyboard';

describe('editor keyboard routing', () => {
  it('preserves native context selector typeahead', () => {
    const select = document.createElement('select');
    expect(editorKeyboardCommand({ key: 'n', target: select })).toBeNull();
    expect(editorKeyboardCommand({ key: 'Delete', target: select })).toBeNull();
  });
  it('lets Escape close the overlay while an input owns focus', () => {
    const input = document.createElement('input');
    expect(isEditableEventTarget(input)).toBe(true);
    expect(editorKeyboardCommand({ key: 'Escape', target: input })).toBe('dismiss-overlay');
  });

  it('keeps native deletion and undo in editable targets while allowing explicit save', () => {
    const textarea = document.createElement('textarea');
    expect(editorKeyboardCommand({ ctrlKey: true, key: 'Backspace', target: textarea })).toBeNull();
    expect(editorKeyboardCommand({ key: 'Delete', target: textarea })).toBeNull();
    expect(editorKeyboardCommand({ ctrlKey: true, key: 'z', target: textarea })).toBeNull();
    expect(editorKeyboardCommand({ ctrlKey: true, key: 's', target: textarea })).toBe('save');
  });

  it('routes non-editable undo, redo, delete, and overlay commands', () => {
    const button = document.createElement('button');
    expect(editorKeyboardCommand({ ctrlKey: true, key: 'z', target: button })).toBe('undo');
    expect(editorKeyboardCommand({ ctrlKey: true, key: 'z', shiftKey: true, target: button })).toBe('redo');
    expect(editorKeyboardCommand({ ctrlKey: true, key: 'Delete', target: button })).toBe('delete');
    expect(editorKeyboardCommand({ key: 'Delete', target: button })).toBe('delete');
    expect(editorKeyboardCommand({ key: 'e', target: button })).toBe('edit-overlay');
    expect(editorKeyboardCommand({ key: 'r', target: button })).toBe('read-overlay');
    expect(editorKeyboardCommand({ key: 't', target: button })).toBe('transcript-overlay');
  });

  it('routes creation shortcuts outside text fields only', () => {
    const button = document.createElement('button');
    const input = document.createElement('input');
    expect(editorKeyboardCommand({ key: 'n', target: button })).toBe('add-line');
    expect(editorKeyboardCommand({ key: 'w', target: button })).toBe('add-word');
    expect(editorKeyboardCommand({ key: 'n', target: input })).toBeNull();
    expect(editorKeyboardCommand({ key: 'w', target: input })).toBeNull();
    expect(editorKeyboardCommand({ ctrlKey: true, key: 'n', target: button })).toBeNull();
  });

  it('routes structural, reprocess, and publish shortcuts outside text fields', () => {
    const button = document.createElement('button');
    expect(editorKeyboardCommand({ altKey: true, key: 's', target: button })).toBe('split-line');
    expect(editorKeyboardCommand({ altKey: true, key: 'l', target: button })).toBe('join-lines');
    expect(editorKeyboardCommand({ altKey: true, key: 'w', target: button })).toBe('join-words');
    expect(editorKeyboardCommand({ altKey: true, key: 'r', target: button })).toBe('reprocess');
    expect(editorKeyboardCommand({ altKey: true, key: 'p', target: button })).toBe('publish');
  });

  it('does not steal structural shortcuts from an editor input', () => {
    const input = document.createElement('input');
    expect(editorKeyboardCommand({ altKey: true, key: 's', target: input })).toBeNull();
    expect(editorKeyboardCommand({ altKey: true, key: 'w', target: input })).toBeNull();
  });
});
