import * as React from "react";
import CodeMirror, { type EditorView } from "@uiw/react-codemirror";
import { createTheme } from "@uiw/codemirror-themes";
import {
  LRLanguage,
  LanguageSupport,
  syntaxHighlighting,
  HighlightStyle,
} from "@codemirror/language";
import { tags as t } from "@lezer/highlight";
import { type Diagnostic, linter as linterExt, lintGutter } from "@codemirror/lint";
import { autocompletion, type CompletionContext } from "@codemirror/autocomplete";
import { matchSorter } from "match-sorter";
import { parser } from "lezer-beancount";

const beancountLanguage = LRLanguage.define({ parser });
const beancount = () => new LanguageSupport(beancountLanguage);

const editorTheme = createTheme({
  theme: "light",
  settings: {
    background: "var(--color-base-100)",
    foreground: "color-mix(in oklab, var(--color-base-content) 85%, transparent)",
    caret: "var(--color-base-content)",
    selection: "color-mix(in oklab, var(--color-base-content) 8%, transparent)",
    selectionMatch: "color-mix(in oklab, var(--color-base-content) 6%, transparent)",
    lineHighlight: "color-mix(in oklab, var(--color-base-content) 3%, transparent)",
    gutterBackground: "var(--color-base-100)",
    gutterForeground: "color-mix(in oklab, var(--color-base-content) 25%, transparent)",
    gutterActiveForeground: "color-mix(in oklab, var(--color-base-content) 50%, transparent)",
  },
  styles: [],
});

const highlightStyle = HighlightStyle.define([
  {
    tag: t.literal,
    color: "color-mix(in oklab, var(--color-base-content) 90%, transparent)",
    fontWeight: "500",
  },
  {
    tag: t.modifier,
    color: "color-mix(in oklab, var(--color-base-content) 85%, transparent)",
    fontWeight: "600",
  },
  {
    tag: t.keyword,
    color: "color-mix(in oklab, var(--color-base-content) 80%, transparent)",
    fontWeight: "500",
  },
  {
    tag: t.variableName,
    color: "color-mix(in oklab, var(--color-base-content) 70%, transparent)",
  },
  {
    tag: t.number,
    color: "color-mix(in oklab, var(--color-base-content) 75%, transparent)",
  },
  {
    tag: t.unit,
    color: "color-mix(in oklab, var(--color-base-content) 55%, transparent)",
    fontWeight: "500",
  },
  {
    tag: t.string,
    color: "color-mix(in oklab, var(--color-base-content) 60%, transparent)",
  },
  {
    tag: t.heading,
    color: "color-mix(in oklab, var(--color-base-content) 75%, transparent)",
    fontWeight: "600",
  },
  {
    tag: t.propertyName,
    color: "color-mix(in oklab, var(--color-base-content) 45%, transparent)",
    fontStyle: "italic",
  },
  {
    tag: t.name,
    color: "color-mix(in oklab, var(--color-base-content) 50%, transparent)",
  },
  {
    tag: t.bool,
    color: "color-mix(in oklab, var(--color-base-content) 55%, transparent)",
  },
  {
    tag: t.tagName,
    color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)",
  },
  {
    tag: t.link,
    color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)",
    textDecoration: "underline",
    textUnderlineOffset: "2px",
    textDecorationColor: "color-mix(in oklab, var(--color-base-content) 20%, transparent)",
  },
  {
    tag: t.lineComment,
    color: "color-mix(in oklab, var(--color-base-content) 35%, transparent)",
    fontStyle: "italic",
  },
  {
    tag: t.arithmeticOperator,
    color: "color-mix(in oklab, var(--color-base-content) 35%, transparent)",
  },
  {
    tag: t.operator,
    color: "color-mix(in oklab, var(--color-base-content) 35%, transparent)",
  },
  {
    tag: t.special(t.operator),
    color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)",
  },
  {
    tag: t.brace,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
  {
    tag: t.paren,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
  {
    tag: t.separator,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
  {
    tag: t.punctuation,
    color: "color-mix(in oklab, var(--color-base-content) 30%, transparent)",
  },
]);

interface AccountInfo {
  name: string;
  type: string;
}

// Autocomplete function for account names using match-sorter for intelligent ranking
const createAccountCompletion = (accounts: AccountInfo[]) => {
  return autocompletion({
    override: [
      (context: CompletionContext) => {
        const word = context.matchBefore(/[\w:-]+/);

        // Only show completions when user is typing
        if (!word || (word.from === word.to && !context.explicit)) {
          return null;
        }

        // Use match-sorter with keys option to sort by account name
        const matchedAccounts = matchSorter(accounts, word.text, {
          keys: ["name"],
          threshold: matchSorter.rankings.CONTAINS,
        });

        const options = matchedAccounts.map((account) => ({
          label: account.name,
          type: "variable",
          detail: account.type,  // Show account type as detail
          apply: account.name,
        }));

        return {
          from: word.from,
          options,
        };
      },
    ],
  });
};

export interface EditorError {
  type: string;
  message: string;
  position?: {
    filename: string;
    line: number;
    column: number;
  };
}

interface EditorProps {
  value?: string;
  errors?: EditorError[] | null;
  accounts: AccountInfo[];
  onChange?: (value: string) => void;
}

function errorsToDiagnostics(errors: EditorError[] | null, view: EditorView): Diagnostic[] {
  if (!errors || errors.length === 0) {
    return [];
  }

  return errors.map((error) => {
    const messageParts = error.message.split(": ");
    const cleanMessage =
      messageParts.length >= 2 ? messageParts.slice(1).join(": ") : error.message;

    if (!error.position) {
      return {
        from: 0,
        to: 1,
        severity: "error" as const,
        message: cleanMessage,
        source: error.type,
      };
    }

    try {
      const line = view.state.doc.line(error.position.line);
      const from = line.from + Math.max(0, error.position.column - 1);
      const to = line.to;

      return {
        from,
        to,
        severity: "error" as const,
        message: cleanMessage,
        source: error.type,
      };
    } catch {
      return {
        from: 0,
        to: 1,
        severity: "error" as const,
        message: cleanMessage,
        source: error.type,
      };
    }
  });
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
        syntaxHighlighting(highlightStyle),
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
