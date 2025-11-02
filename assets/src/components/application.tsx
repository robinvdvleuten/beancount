import * as React from "react";
import CodeMirror, { type EditorView } from "@uiw/react-codemirror";
import { createTheme } from "@uiw/codemirror-themes";
import {
  type Diagnostic,
  linter as linterExt,
  lintGutter,
} from "@codemirror/lint";
import {
  ArrowDownTrayIcon,
  DocumentCurrencyDollarIcon,
} from "@heroicons/react/24/outline";

interface ErrorPosition {
  filename: string;
  line: number;
  column: number;
}

interface ErrorJSON {
  type: string;
  message: string;
  position?: ErrorPosition;
  details?: Record<string, any>;
}

interface SourceResponse {
  filepath: string;
  source: string;
  errors: ErrorJSON[] | null;
}

function errorsToDiagnostics(
  errors: ErrorJSON[] | null,
  view: EditorView,
): Diagnostic[] {
  if (!errors || errors.length === 0) {
    return [];
  }

  return errors.map((error) => {
    const messageParts = error.message.split(": ");
    const cleanMessage =
      messageParts.length >= 2
        ? messageParts.slice(1).join(": ")
        : error.message;

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
    } catch (e) {
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

const theme = createTheme({
  theme: "light",
  settings: {
    background: "var(--color-base-100)",
    foreground: "var(	--color-base-content)",
    caret: "var(	--color-base-content)",
    selection:
      "color-mix(in oklab, var(--color-primary) 5%, var(--color-base-100))",
    selectionMatch:
      "color-mix(in oklab, var(--color-primary) 5%, var(--color-base-100))",
    lineHighlight:
      "color-mix(in oklab, var(--color-primary) 10%, var(--color-base-100))",
    gutterBackground: "var(--color-base-100)",
    gutterForeground:
      "color-mix(in oklab, var(--color-base-content) 50%, transparent)",
    gutterActiveForeground: "var(	--color-base-content)",
  },
  styles: [],
});

interface ApplicationProps {
  meta: { version: string; commitSHA: string };
}

const Application: React.FC<ApplicationProps> = ({ meta }) => {
  const [filepath, setFilepath] = React.useState<string | null>(null);
  const [source, setSource] = React.useState<string>();
  const [errors, setErrors] = React.useState<ErrorJSON[] | null>(null);

  const linter = React.useMemo(() => {
    return linterExt((view) => errorsToDiagnostics(errors, view));
  }, [errors]);

  const handleValueChange = React.useCallback((value: string) => {
    setSource(value);
  }, []);

  const handleSaveClick = React.useCallback(async () => {
    const response = await fetch("/api/source", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        source,
      }),
    });

    if (!response.ok) {
      console.error("Unable to save ledger: ", response.status);
      return;
    }

    const result: SourceResponse = await response.json();
    setFilepath(result.filepath);
    setSource(result.source);
    setErrors(result.errors);
  }, [source]);

  React.useEffect(() => {
    async function fetchSource() {
      setSource(undefined);
      setErrors(null);

      const response = await fetch("/api/source");
      if (!response.ok) {
        console.error("Unable to loader ledger: ", response.status);
        return;
      }

      const result: SourceResponse = await response.json();

      if (!ignore) {
        setFilepath(result.filepath);
        setSource(result.source);
        setErrors(result.errors);
      }
    }

    let ignore = false;
    fetchSource();

    return () => {
      ignore = true;
    };
  }, []);

  return (
    <div className="flex h-screen flex-col">
      <header className="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div className="flex items-center gap-3">
          <div className="text-primary">
            <DocumentCurrencyDollarIcon className="size-8" />
          </div>
          <div className="text-base-content">
            <h1 className="text-xl font-semibold">Beancount Editor</h1>
            <p className="text-sm text-base-content/50">{filepath ?? "..."}</p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button className="btn" onClick={handleSaveClick}>
            <ArrowDownTrayIcon className="size-4" />
            Save
          </button>
        </div>
      </header>

      <div className="flex-1 overflow-auto">
        <CodeMirror
          value={source}
          theme={theme}
          extensions={[linter, lintGutter()]}
          onChange={handleValueChange}
          height="100%"
        />
      </div>

      <footer className="flex items-center justify-between border-t border-base-300 px-6 py-2">
        <div className="text-xs text-base-content/70">
          {meta.version}
          {meta.commitSHA && ` (${meta.commitSHA})`}
        </div>
      </footer>
    </div>
  );
};

export default Application;
