import {
  type Component,
  createResource,
  createSignal,
  Switch,
  Match,
  Show,
  For,
} from "solid-js";
import ArrowDownTrayIcon from "heroicons/24/solid/arrow-down-tray.svg?component-solid";
import DocumentCurrencyDollarIcon from "heroicons/24/solid/document-currency-dollar.svg?component-solid";
import ChevronDownIcon from "heroicons/24/solid/chevron-down.svg?component-solid";
import type { AccountInfo, EditorError } from "../types";
import EditorComp from "../components/editor";
import { meta, files } from "virtual:globals";

interface SourceResponse {
  filepath: string;
  source: string;
  errors: EditorError[] | null;
}

interface AccountsResponse {
  accounts: AccountInfo[];
}

const fetchSourceForFile = async (
  filepath?: string,
): Promise<SourceResponse> => {
  const url = filepath
    ? `/api/source?filepath=${encodeURIComponent(filepath)}`
    : "/api/source";
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Failed to fetch source: ${response.statusText}`);
  }
  return (await response.json()) as SourceResponse;
};

const fetchAccounts = async (): Promise<AccountsResponse> => {
  const response = await fetch("/api/accounts");
  if (!response.ok) {
    throw new Error(`Failed to fetch accounts: ${response.statusText}`);
  }
  return (await response.json()) as AccountsResponse;
};

const Editor: Component = () => {
  // Track currently selected file
  const [currentFile, setCurrentFile] = createSignal<string>(files.root);

  // Fetch source for the current file
  const [sourceData, { mutate: mutateSource }] = createResource(
    currentFile,
    fetchSourceForFile,
  );
  const [accountsData, { refetch: refetchAccounts }] =
    createResource(fetchAccounts);

  // Local editing state - tracks unsaved changes
  const [editedSource, setEditedSource] = createSignal<string | undefined>(
    undefined,
  );
  // Local errors state - updated after save
  const [errors, setErrors] = createSignal<EditorError[] | null>(null);

  // All available files (root + includes)
  const allFiles = () => [files.root, ...files.includes];

  // Handle file selection from dropdown
  const handleFileSelect = (filepath: string) => {
    if (filepath !== currentFile()) {
      setCurrentFile(filepath);
      setEditedSource(undefined);
      setErrors(null);
    }
  };

  const handleValueChange = (value: string) => {
    setEditedSource(value);
  };

  // Use edited source if available, otherwise use fetched source
  const currentSource = () => editedSource() ?? sourceData()?.source;

  const handleSaveClick = async () => {
    const response = await fetch("/api/source", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        filepath: currentFile(),
        source: currentSource(),
      }),
    });

    if (!response.ok) {
      console.error("Unable to save ledger: ", response.status);
      return;
    }

    const result = (await response.json()) as SourceResponse;

    // Update the resource with the saved data
    mutateSource(result);
    // Sync edited source with saved source
    setEditedSource(result.source);
    // Update errors from save response
    setErrors(result.errors);

    // Reload accounts to pick up new accounts from the saved file
    await refetchAccounts();
  };

  // Sync errors from initial fetch
  const currentErrors = () => errors() ?? sourceData()?.errors ?? null;

  // Extract just the filename from a path for display
  const displayFilename = (filepath: string) => {
    const parts = filepath.split("/");
    return parts[parts.length - 1];
  };

  return (
    <>
      <header class="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div class="flex items-center gap-3">
          <div class="text-primary">
            <DocumentCurrencyDollarIcon class="size-8" />
          </div>
          <div class="text-base-content">
            <h1 class="text-xl font-semibold">Beancount Editor</h1>
            <Show
              when={files.includes.length > 0}
              fallback={
                <p class="text-sm text-base-content/50">
                  {sourceData()?.filepath ?? "..."}
                </p>
              }
            >
              <details class="dropdown">
                <summary class="btn btn-ghost btn-sm gap-1 px-0 font-normal text-base-content/50">
                  {displayFilename(currentFile())}
                  <ChevronDownIcon class="size-3" />
                </summary>
                <ul class="menu dropdown-content bg-base-100 rounded-box z-10 w-64 p-2 shadow-lg">
                  <For each={allFiles()}>
                    {(filepath) => (
                      <li>
                        <a
                          class={filepath === currentFile() ? "active" : ""}
                          onClick={() => handleFileSelect(filepath)}
                        >
                          {displayFilename(filepath)}
                        </a>
                      </li>
                    )}
                  </For>
                </ul>
              </details>
            </Show>
          </div>
        </div>

        <div class="flex items-center gap-2">
          <button
            class="btn"
            onClick={() => void handleSaveClick()}
            disabled={meta.readOnly || sourceData.loading}
          >
            <ArrowDownTrayIcon class="size-4" />
            Save
          </button>
        </div>
      </header>

      <div class="flex-1 overflow-auto">
        <Switch>
          <Match when={sourceData.loading}>
            <div class="flex items-center justify-center py-12">
              <span class="loading loading-spinner loading-lg" />
            </div>
          </Match>

          <Match when={sourceData.error as Error | undefined}>
            {(error) => (
              <div class="alert alert-error m-6" role="alert">
                <span>Error: {error().message}</span>
              </div>
            )}
          </Match>

          <Match when={sourceData()}>
            <EditorComp
              value={currentSource()}
              errors={currentErrors()}
              accounts={accountsData()?.accounts ?? []}
              filepath={sourceData()?.filepath ?? null}
              onChange={handleValueChange}
            />
          </Match>
        </Switch>
      </div>
    </>
  );
};

export default Editor;
