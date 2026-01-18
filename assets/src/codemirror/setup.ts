import { EditorView, keymap } from "@codemirror/view";
import { EditorState, type Extension } from "@codemirror/state";
import { indentWithTab } from "@codemirror/commands";

export type OnChangeRef = { current: ((value: string) => void) | undefined };

interface EditorViewConfig {
  parent: HTMLElement;
  value?: string;
  extensions: Extension[];
  onChangeRef?: OnChangeRef;
}

export function createUpdateListener(onChangeRef?: OnChangeRef): Extension {
  return EditorView.updateListener.of((update) => {
    if (update.docChanged && onChangeRef?.current) {
      onChangeRef.current(update.state.doc.toString());
    }
  });
}

export function createEditorView(config: EditorViewConfig): EditorView {
  const { parent, value = "", extensions, onChangeRef } = config;

  const state = EditorState.create({
    doc: value,
    extensions: [
      ...extensions,
      keymap.of([indentWithTab]),
      createUpdateListener(onChangeRef),
    ],
  });

  return new EditorView({
    state,
    parent,
  });
}
