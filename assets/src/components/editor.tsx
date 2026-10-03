import { createEffect, createMemo, onCleanup, onMount, on } from "solid-js";
import { linter as linterExt, lintGutter } from "@codemirror/lint";
import { StateEffect } from "@codemirror/state";
import { lineNumbers, EditorView, type KeyBinding } from "@codemirror/view";
import type { AccountInfo, EditorError } from "../types";
import { beancount } from "../codemirror/language";
import { editorTheme, beancountSyntaxHighlighting } from "../codemirror/theme";
import { errorsToDiagnostics } from "../codemirror/error-diagnostics";
import { createAccountCompletion } from "../codemirror/autocomplete";
import { createEditorKeymap, createEditorView, createUpdateListener } from "../codemirror/setup";

interface EditorProps {
  value?: string;
  errors?: EditorError[] | null;
  accounts: AccountInfo[];
  filepath?: string | null;
  /** A line to scroll to and select once the document holding it shows */
  line?: number;
  onChange?: (value: string) => void;
  onSaveRequest?: () => void;
}

const Editor = (props: EditorProps) => {
  let editorRef: HTMLDivElement | undefined = undefined;
  let viewRef: EditorView | null = null;

  const saveKeyBinding = (): KeyBinding => ({
    key: "Mod-s",
    run: () => {
      props.onSaveRequest?.();
      return true;
    },
  });

  const linter = createMemo(
    () => {
      // Track errors and filepath dependencies to recreate linter when they change
      const _errors = props.errors;
      const _filepath = props.filepath;
      return linterExt((view) => errorsToDiagnostics(_errors ?? null, view, _filepath ?? null));
    },
    undefined,
    { equals: false },
  );

  const accountCompletion = createMemo(() => {
    return createAccountCompletion(props.accounts);
  });

  // Create editor view once on mount
  onMount(() => {
    if (!editorRef) return;

    const view = createEditorView({
      parent: editorRef,
      value: props.value ?? "",
      extensions: [
        lineNumbers(),
        beancount(),
        beancountSyntaxHighlighting,
        editorTheme,
        linter(),
        lintGutter(),
        accountCompletion(),
      ],
      onChange: (value) => props.onChange?.(value),
      keyBindings: [saveKeyBinding()],
    });

    viewRef = view;

    onCleanup(() => {
      view.destroy();
      viewRef = null;
    });
  });

  // Update editor content when value prop changes externally
  createEffect(() => {
    const view = viewRef;
    if (!view || props.value === undefined) return;

    const currentValue = view.state.doc.toString();
    if (props.value !== currentValue) {
      view.dispatch({
        changes: { from: 0, to: currentValue.length, insert: props.value },
      });
    }
  });

  // Reveal the requested line once per file and line, after the document
  // holding it is shown
  let revealed: string | undefined;
  createEffect(() => {
    const view = viewRef;
    const line = props.line;
    const key = `${props.filepath ?? ""}:${line}`;
    if (!view || props.value === undefined || line === undefined || key === revealed) return;
    if (line < 1 || line > view.state.doc.lines) return;
    const target = view.state.doc.line(line);
    view.dispatch({
      selection: { anchor: target.from, head: target.to },
      effects: EditorView.scrollIntoView(target.from, { y: "center" }),
    });
    view.focus();
    revealed = key;
  });

  // Reconfigure extensions when linter, completion or callbacks change
  // Use defer: true to skip the initial run - the editor is created with all extensions in onMount
  createEffect(
    on(
      [linter, accountCompletion, () => props.onChange, () => props.onSaveRequest],
      () => {
        const view = viewRef;
        if (!view) return;

        view.dispatch({
          effects: StateEffect.reconfigure.of([
            lineNumbers(),
            beancount(),
            beancountSyntaxHighlighting,
            editorTheme,
            linter(),
            lintGutter(),
            accountCompletion(),
            createEditorKeymap([saveKeyBinding()]),
            createUpdateListener((value) => props.onChange?.(value)),
          ]),
        });
      },
      { defer: true },
    ),
  );

  return <div ref={editorRef} class="h-full" />;
};

export default Editor;
