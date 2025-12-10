import * as React from "react";
import CodeMirror from "@uiw/react-codemirror";
import { linter as linterExt, lintGutter } from "@codemirror/lint";
import type { AccountInfo, EditorError } from "../types";
import { beancount } from "../codemirror/language";
import { editorTheme, tooltipTheme, beancountSyntaxHighlighting } from "../codemirror/theme";
import { errorsToDiagnostics } from "../codemirror/error-diagnostics";
import { createAccountCompletion } from "../codemirror/autocomplete";

interface EditorProps {
  value?: string;
  errors?: EditorError[] | null;
  accounts: AccountInfo[];
  onChange?: (value: string) => void;
}

const Editor: React.FC<EditorProps> = ({ value, errors, accounts, onChange }) => {
  const linter = React.useMemo(() => {
    return linterExt((view) => errorsToDiagnostics(errors ?? null, view));
  }, [errors]);

  const accountCompletion = React.useMemo(() => {
    return createAccountCompletion(accounts);
  }, [accounts]);

  return (
    <CodeMirror
      value={value}
      theme={editorTheme}
      extensions={[
        beancount(),
        beancountSyntaxHighlighting,
        tooltipTheme,
        linter,
        lintGutter(),
        accountCompletion,
      ]}
      onChange={onChange}
      height="100%"
    />
  );
};

export default Editor;
