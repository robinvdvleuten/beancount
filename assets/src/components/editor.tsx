import * as React from "react";
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

const Editor: React.FC<EditorProps> = ({ value, errors, accounts, onChange }) => {
  const editorRef = React.useRef<HTMLDivElement>(null);
  const viewRef = React.useRef<EditorView | null>(null);

  const linter = React.useMemo(() => {
    return linterExt((view) => errorsToDiagnostics(errors ?? null, view));
  }, [errors]);

  const accountCompletion = React.useMemo(() => {
    return createAccountCompletion(accounts);
  }, [accounts]);

  // Create editor view once on mount
  React.useEffect(() => {
    if (!editorRef.current) return;

    const view = createEditorView({
      parent: editorRef.current,
      value: value ?? "",
      extensions: [
        lineNumbers(),
        beancount(),
        beancountSyntaxHighlighting,
        editorTheme,
        linter,
        lintGutter(),
        accountCompletion,
      ],
      onChange,
    });

    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, []); // Empty deps - only create once

  // Update editor content when value prop changes externally
  React.useEffect(() => {
    const view = viewRef.current;
    if (!view || value === undefined) return;

    const currentValue = view.state.doc.toString();
    if (value !== currentValue) {
      view.dispatch({
        changes: { from: 0, to: currentValue.length, insert: value },
      });
    }
  }, [value]);

  // Reconfigure extensions when linter or completion changes
  React.useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    view.dispatch({
      effects: StateEffect.reconfigure.of([
        lineNumbers(),
        beancount(),
        beancountSyntaxHighlighting,
        editorTheme,
        linter,
        lintGutter(),
        accountCompletion,
      ]),
    });
  }, [linter, accountCompletion]);

  return <div ref={editorRef} className="h-full" />;
};

export default Editor;
