import { createEffect, createMemo, onCleanup, onMount, on } from "solid-js";
import { linter as linterExt, lintGutter } from "@codemirror/lint";
import { StateEffect } from "@codemirror/state";
import { lineNumbers, type EditorView } from "@codemirror/view";
import type { AccountInfo, EditorError } from "../types";
import { beancount } from "../codemirror/language";
import { editorTheme, beancountSyntaxHighlighting } from "../codemirror/theme";
import { errorsToDiagnostics } from "../codemirror/error-diagnostics";
import { createAccountCompletion } from "../codemirror/autocomplete";
import { createEditorView } from "../codemirror/setup";

interface EditorProps {
  value?: string;
  errors?: EditorError[] | null;
  accounts: AccountInfo[];
  onChange?: (value: string) => void;
}

const Editor = (props: EditorProps) => {
  let editorRef: HTMLDivElement | undefined;
  let viewRef: EditorView | null = null;

  const linter = createMemo(
    () => {
      // Track errors dependency to recreate linter when errors change
      const _errors = props.errors;
      return linterExt((view) => errorsToDiagnostics(_errors ?? null, view));
    },
    undefined,
    { equals: false }
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
      onChange: props.onChange,
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

  // Reconfigure extensions when linter or completion changes
  createEffect(
    on([linter, accountCompletion], () => {
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
        ]),
      });
    })
  );

  return <div ref={editorRef} class="h-full" />;
};

export default Editor;
