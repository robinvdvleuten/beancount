import * as React from "react";
import {
  ArrowDownTrayIcon,
  DocumentCurrencyDollarIcon,
} from "@heroicons/react/24/outline";
import Editor, { type EditorError } from "./editor";

interface SourceResponse {
  filepath: string;
  source: string;
  errors: EditorError[] | null;
}

interface ApplicationProps {
  meta: { version: string; commitSHA: string };
}

const Application: React.FC<ApplicationProps> = ({ meta }) => {
  const [filepath, setFilepath] = React.useState<string | null>(null);
  const [source, setSource] = React.useState<string>();
  const [errors, setErrors] = React.useState<EditorError[] | null>(null);

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
        <Editor value={source} errors={errors} onChange={handleValueChange} />
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
