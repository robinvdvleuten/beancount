import { EditorView, keymap, type KeyBinding } from "@codemirror/view";
import { EditorState, type Extension } from "@codemirror/state";
import { indentWithTab } from "@codemirror/commands";

interface EditorViewConfig {
  parent: HTMLElement;
  value?: string;
  extensions: Extension[];
  onChange?: (value: string) => void;
  keyBindings?: KeyBinding[];
}

export function createUpdateListener(
  onChange?: (value: string) => void,
): Extension {
  return EditorView.updateListener.of((update) => {
    if (update.docChanged && onChange) {
      onChange(update.state.doc.toString());
    }
  });
}

export function createEditorKeymap(keyBindings: KeyBinding[] = []): Extension {
  return keymap.of([indentWithTab, ...keyBindings]);
}

export function createEditorView(config: EditorViewConfig): EditorView {
  const { parent, value = "", extensions, onChange, keyBindings } = config;

  const state = EditorState.create({
    doc: value,
    extensions: [
      ...extensions,
      createEditorKeymap(keyBindings),
      createUpdateListener(onChange),
    ],
  });

  return new EditorView({
    state,
    parent,
  });
}
