import { EditorView, keymap } from "@codemirror/view";
import { EditorState, type Extension } from "@codemirror/state";
import { indentWithTab } from "@codemirror/commands";

interface EditorViewConfig {
  parent: HTMLElement;
  value?: string;
  extensions: Extension[];
  onChange?: (value: string) => void;
}

export function createEditorView(config: EditorViewConfig): EditorView {
  const { parent, value = "", extensions, onChange } = config;

  const updateListener = EditorView.updateListener.of((update) => {
    if (update.docChanged && onChange) {
      onChange(update.state.doc.toString());
    }
  });

  const state = EditorState.create({
    doc: value,
    extensions: [
      ...extensions,
      keymap.of([indentWithTab]),
      updateListener,
    ],
  });

  return new EditorView({
    state,
    parent,
  });
}
