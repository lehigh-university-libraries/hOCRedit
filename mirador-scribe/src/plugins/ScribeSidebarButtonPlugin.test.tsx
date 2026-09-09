import { describe, expect, it } from 'vitest';
import scribeSidebarButtonPlugin from './ScribeSidebarButtonPlugin';

describe('Scribe sidebar translations', () => {
  it('labels repeat provider work as a whole-page reprocess, never a per-line retranscription', () => {
    const translations = scribeSidebarButtonPlugin.config.translations.en as Record<string, string>;
    expect(translations.scribeEditorReprocess).toBe('Reprocess Page');
    expect(Object.keys(translations).filter((key) => key.startsWith('scribeEditorTranscribe'))).toEqual([]);
  });
});
