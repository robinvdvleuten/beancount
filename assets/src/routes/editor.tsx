import {
  type Component,
  createResource,
  createSignal,
  Switch,
  Match,
} from "solid-js";
import ArrowDownTrayIcon from "heroicons/24/solid/arrow-down-tray.svg?component-solid";
import DocumentCurrencyDollarIcon from "heroicons/24/solid/document-currency-dollar.svg?component-solid";
import type { AccountInfo, EditorError } from "../types";
import EditorComp from "../components/editor";
import { meta } from "virtual:globals";

interface SourceResponse {
  filepath: string;
  source: string;
  errors: EditorError[] | null;
}

interface AccountsResponse {
  accounts: AccountInfo[];
}

const fetchSource = async (): Promise<SourceResponse> => {
  const response = await fetch("/api/source");
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
  const [sourceData, { mutate: mutateSource }] = createResource(fetchSource);
  const [accountsData, { refetch: refetchAccounts }] =
    createResource(fetchAccounts);

  // Local editing state - tracks unsaved changes
  const [editedSource, setEditedSource] = createSignal<string | undefined>(
    undefined,
  );
  // Local errors state - updated after save
  const [errors, setErrors] = createSignal<EditorError[] | null>(null);

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

  return (
    <>
      <header class="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div class="flex items-center gap-3">
          <div class="text-primary">
            <DocumentCurrencyDollarIcon class="size-8" />
          </div>
          <div class="text-base-content">
            <h1 class="text-xl font-semibold">Beancount Editor</h1>
            <p class="text-sm text-base-content/50">
              {sourceData()?.filepath ?? "..."}
            </p>
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
