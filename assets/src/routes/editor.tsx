import { type Component, createSignal, onMount } from "solid-js";
import ArrowDownTrayIcon from "heroicons/24/solid/arrow-down-tray.svg?component-solid"
import DocumentCurrencyDollarIcon from "heroicons/24/solid/document-currency-dollar.svg?component-solid"
import type { AccountInfo, EditorError } from "../types";
import EditorComp from "../components/editor";
import { meta } from "virtual:globals";

interface SourceResponse {
  filepath: string;
  source: string;
  errors: EditorError[] | null;
}

const Editor: Component = () => {
  const [filepath, setFilepath] = createSignal<string | null>(null);
  const [source, setSource] = createSignal<string>();
  const [errors, setErrors] = createSignal<EditorError[] | null>(null);
  const [accounts, setAccounts] = createSignal<AccountInfo[]>([]);

  const handleValueChange = (value: string) => {
    setSource(value);
  };

  const fetchAccounts = async () => {
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
  };

  const handleSaveClick = async () => {
    const response = await fetch("/api/source", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        source: source(),
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
  };

  onMount(() => {
    async function fetchSource() {
      setSource(undefined);
      setErrors(null);

      const response = await fetch("/api/source");
      if (!response.ok) {
        console.error("Unable to loader ledger: ", response.status);
        return;
      }

      const result: SourceResponse = await response.json();

      setFilepath(result.filepath);
      setSource(result.source);
      setErrors(result.errors);
    }

    fetchSource();
    fetchAccounts();
  });

  return (
    <>
      <header class="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div class="flex items-center gap-3">
          <div class="text-primary">
            <DocumentCurrencyDollarIcon class="size-8" />
          </div>
          <div class="text-base-content">
            <h1 class="text-xl font-semibold">Beancount Editor</h1>
            <p class="text-sm text-base-content/50">{filepath() ?? "..."}</p>
          </div>
        </div>

        <div class="flex items-center gap-2">
          <button
            class="btn"
            onClick={handleSaveClick}
            disabled={meta.readOnly}
          >
            <ArrowDownTrayIcon class="size-4" />
            Save
          </button>
        </div>
      </header>

      <div class="flex-1 overflow-auto">
        <EditorComp value={source()} errors={errors()} accounts={accounts()} onChange={handleValueChange} />
      </div>
    </>
  );
};

export default Editor;
