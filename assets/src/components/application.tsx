import * as React from "react";
import {
  ArrowDownTrayIcon,
  DocumentCurrencyDollarIcon,
} from "@heroicons/react/24/outline";
import type { AccountInfo, EditorError } from "../types";
import Editor from "./editor";

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
  const [accounts, setAccounts] = React.useState<AccountInfo[]>([]);

  const handleValueChange = React.useCallback((value: string) => {
    setSource(value);
  }, []);

  const fetchAccounts = React.useCallback(async () => {
    try {
      const response = await fetch("/api/accounts");
      if (!response.ok) {
        throw new Error(`Failed to fetch accounts: ${response.statusText}`);
      }
      const data = await response.json();
      // Keep full objects with type information
      setAccounts(data.accounts);
    } catch (error) {
      console.error("Failed to fetch accounts:", error);
      setAccounts([]);
    }
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

    // Reload accounts to pick up new accounts from the saved file
    await fetchAccounts();
  }, [source, fetchAccounts]);

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
    fetchAccounts();

    return () => {
      ignore = true;
    };
  }, [fetchAccounts]);

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
        <Editor value={source} errors={errors} accounts={accounts} onChange={handleValueChange} />
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
