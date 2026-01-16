import { createEffect, createMemo, onCleanup, onMount, on } from "solid-js";
import { linter as linterExt, lintGutter } from "@codemirror/lint";
import { StateEffect } from "@codemirror/state";
import { lineNumbers, EditorView, keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import type { AccountInfo, EditorError } from "../types";
import { beancount } from "../codemirror/language";
import { editorTheme, beancountSyntaxHighlighting } from "../codemirror/theme";
import { errorsToDiagnostics } from "../codemirror/error-diagnostics";
import { createAccountCompletion } from "../codemirror/autocomplete";
import { createEditorView, createUpdateListener } from "../codemirror/setup";

interface EditorProps {
  value?: string;
  errors?: EditorError[] | null;
  accounts: AccountInfo[];
  filepath?: string | null;
  onChange?: (value: string) => void;
}

const Editor = (props: EditorProps) => {
  let editorRef: HTMLDivElement | undefined;
  let viewRef: EditorView | null = null;

  const linter = createMemo(
    () => {
      // Track errors and filepath dependencies to recreate linter when they change
      const _errors = props.errors;
      const _filepath = props.filepath;
      return linterExt((view) =>
        errorsToDiagnostics(_errors ?? null, view, _filepath ?? null),
      );
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

  // Reconfigure extensions when linter, completion or onChange changes
  // Use defer: true to skip the initial run - the editor is created with all extensions in onMount
  createEffect(
    on(
      [linter, accountCompletion, () => props.onChange],
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
            keymap.of([indentWithTab]),
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
