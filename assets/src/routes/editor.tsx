import {
  type Component,
  createResource,
  createSignal,
  createEffect,
  Switch,
  Match,
  Show,
  For,
  on,
  onCleanup,
  onMount,
  untrack,
} from "solid-js";
import ArrowDownTrayIcon from "heroicons/24/solid/arrow-down-tray.svg?component-solid";
import ChevronDownIcon from "heroicons/24/solid/chevron-down.svg?component-solid";
import type { AccountInfo, EditorError } from "../types";
import EditorComp from "../components/editor";
import { useSearchParams } from "@solidjs/router";
import { meta } from "virtual:globals";
import { useFileChange } from "../hooks/useFileChange";
import { refetchLedgerErrors } from "../lib/errors";
import { useToast } from "../hooks/useToast";

interface Files {
  root: string;
  includes: string[];
}

interface SourceResponse {
  source: string;
  fingerprint: string;
  errors: EditorError[] | null;
  files: Files;
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

const fetchSourceForFile = async (filepath: string): Promise<SourceResponse> => {
  const url = `/api/source?filepath=${encodeURIComponent(filepath)}`;
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
  // Initial fetch to get root file and files list
  const [initialData, { refetch: refetchInitial }] = createResource(fetchSource);

  // Track currently selected file (initialized from initial fetch)
  const [currentFile, setCurrentFile] = createSignal<string | undefined>(undefined);

  // Track available files (updated from API responses)
  const [currentFiles, setCurrentFiles] = createSignal<Files | undefined>(undefined);

  // Track last known fingerprint for conflict detection
  const [fingerprint, setFingerprint] = createSignal<string | undefined>(undefined);

  // Conflict modal state
  const [showConflictModal, setShowConflictModal] = createSignal(false);
  let conflictModalRef: HTMLDialogElement | undefined;
  let fileDropdownRef: HTMLDetailsElement | undefined;

  // ?file= and ?line= open the editor at a line, such as an error's
  const [searchParams] = useSearchParams<{ file?: string; line?: string }>();

  // The requested file when it is the root or one of its includes, else
  // the root
  const requestedFile = (files: Files) => {
    const file = searchParams.file;
    return file && (file === files.root || files.includes.includes(file)) ? file : files.root;
  };

  // Initialize currentFile, currentFiles, and fingerprint from initial fetch
  createEffect(() => {
    const data = initialData();
    if (data && currentFile() === undefined) {
      setCurrentFile(requestedFile(data.files));
      setCurrentFiles(data.files);
      setFingerprint(data.fingerprint);
    }
  });

  // The requested line, while its file is the one shown
  const requestedLine = () => {
    const files = currentFiles();
    const line = Number(searchParams.line);
    if (!files || !Number.isInteger(line) || currentFile() !== requestedFile(files))
      return undefined;
    return line;
  };

  // Fetch source for a specific file (used when switching files)
  const [fileData, { mutate: mutateFileData }] = createResource(
    // Only fetch when currentFile changes AND it's different from initial root
    () => {
      const file = currentFile();
      const initial = initialData();
      if (!file || !initial) return undefined;
      // Don't refetch if it's the initial root file we already have
      if (file === initial.files.root) return undefined;
      return file;
    },
    fetchSourceForFile,
  );

  // Update fingerprint when fileData changes
  createEffect(() => {
    const data = fileData();
    if (data) {
      setFingerprint(data.fingerprint);
    }
  });

  const [accountsData, { refetch: refetchAccounts }] = createResource(fetchAccounts);

  // Success toast for save
  const saveToast = useToast();

  // File change detection via SSE
  const fileChange = useFileChange({
    getLastFingerprint: () => fingerprint(),
    onReload: () => {
      void refetchInitial();
      void refetchAccounts();
      setEditedSource(undefined);
      setErrors(null);
    },
  });

  // Local editing state - tracks unsaved changes
  const [editedSource, setEditedSource] = createSignal<string | undefined>(undefined);
  // Local errors state - updated after save
  const [errors, setErrors] = createSignal<EditorError[] | null>(null);

  // Get the current source data (from file fetch or initial fetch)
  const sourceData = () => {
    const file = currentFile();
    const initial = initialData();
    if (!file || !initial) return undefined;

    // If we fetched a specific file, use that data
    const fetched = fileData();
    if (fetched && file !== initial.files.root) {
      return fetched;
    }

    // Otherwise use initial data
    return initial;
  };

  // All available files (root + includes)
  const allFiles = () => {
    const files = currentFiles();
    if (!files) return [];
    return [files.root, ...files.includes];
  };

  // Handle file selection from dropdown
  const handleFileSelect = (filepath: string) => {
    if (filepath !== currentFile()) {
      setCurrentFile(filepath);
      setEditedSource(undefined);
      setErrors(null);
    }
    closeFileDropdown();
  };

  // Follow a link to another file while the editor is open
  createEffect(
    on(
      () => searchParams.file,
      () => {
        const files = untrack(currentFiles);
        if (files) handleFileSelect(requestedFile(files));
      },
      { defer: true },
    ),
  );

  const closeFileDropdown = () => {
    if (fileDropdownRef) {
      fileDropdownRef.open = false;
    }
  };

  const handleFileDropdownPointerDown = (event: PointerEvent) => {
    const target = event.target;
    if (target instanceof Node && !fileDropdownRef?.contains(target)) {
      closeFileDropdown();
    }
  };

  onMount(() => {
    document.addEventListener("pointerdown", handleFileDropdownPointerDown);
  });

  onCleanup(() => {
    document.removeEventListener("pointerdown", handleFileDropdownPointerDown);
  });

  const handleValueChange = (value: string) => {
    setEditedSource(value);
  };

  // Use edited source if available, otherwise use fetched source
  const currentSource = () => editedSource() ?? sourceData()?.source;

  // Save with optional force flag (to overwrite conflicts)
  const doSave = async (force: boolean) => {
    const response = await fetch("/api/source", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        filepath: currentFile(),
        source: currentSource(),
        fingerprint: fingerprint(),
        force,
      }),
    });

    // Handle conflict
    if (response.status === 409) {
      setShowConflictModal(true);
      conflictModalRef?.showModal();
      return;
    }

    if (!response.ok) {
      console.error("Unable to save ledger: ", response.status);
      return;
    }

    const result = (await response.json()) as SourceResponse;

    // Update the file data with the saved data
    mutateFileData(result);
    // Sync edited source with saved source
    setEditedSource(result.source);
    // Update errors from save response
    setErrors(result.errors);
    // Update files list (may have changed if includes were added/removed)
    setCurrentFiles(result.files);
    // Update fingerprint
    setFingerprint(result.fingerprint);
    // Mark as saved so SSE event with this fingerprint is ignored
    fileChange.markSaved(result.fingerprint);

    // Show success toast
    void saveToast.show();

    // The sidebar's error count follows the saved ledger
    refetchLedgerErrors();

    // Reload accounts to pick up new accounts from the saved file
    await refetchAccounts();
  };

  const handleSaveRequest = () => {
    if (meta.readOnly || isLoading()) return;
    void doSave(false);
  };

  const handleForceOverwrite = () => {
    conflictModalRef?.close();
    setShowConflictModal(false);
    void doSave(true);
  };

  const handleCancelOverwrite = () => {
    conflictModalRef?.close();
    setShowConflictModal(false);
  };

  // Sync errors from initial fetch
  const currentErrors = () => errors() ?? sourceData()?.errors ?? null;

  // Extract just the filename from a path for display
  const displayFilename = (filepath: string) => {
    const parts = filepath.split("/");
    return parts[parts.length - 1];
  };

  // Loading state
  const isLoading = () => initialData.loading || fileData.loading;

  // Error state
  const loadError = () =>
    (initialData.error as Error | undefined) ?? (fileData.error as Error | undefined);

  return (
    <>
      <div class="navbar min-h-0 border-b border-base-300 bg-base-100 px-4 py-2">
        <div class="navbar-start">
          <Show
            when={currentFiles() && currentFiles()!.includes.length > 0}
            fallback={
              <span class="text-sm text-base-content/70" aria-label="Current file">
                {currentFile() ? displayFilename(currentFile()!) : "..."}
              </span>
            }
          >
            <details ref={(el) => (fileDropdownRef = el)} class="dropdown">
              <summary class="btn gap-1 btn-ghost px-2 btn-sm" aria-label="Select file">
                {currentFile() ? displayFilename(currentFile()!) : "..."}
                <ChevronDownIcon class="size-3" />
              </summary>
              <ul class="menu dropdown-content z-10 w-64 rounded-box bg-base-100 p-2 shadow-lg">
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

        <div class="navbar-end">
          <button
            class="btn btn-sm"
            onClick={handleSaveRequest}
            disabled={meta.readOnly || isLoading()}
          >
            <ArrowDownTrayIcon class="size-4" />
            Save
          </button>
        </div>
      </div>

      <div class="flex-1 overflow-auto">
        <Switch>
          <Match when={isLoading()}>
            <div class="flex items-center justify-center py-12">
              <span class="loading loading-lg loading-spinner" />
            </div>
          </Match>

          <Match when={loadError()}>
            {(error) => (
              <div class="m-6 alert alert-error" role="alert">
                <span>Error: {error().message}</span>
              </div>
            )}
          </Match>

          <Match when={sourceData()}>
            <EditorComp
              value={currentSource()}
              errors={currentErrors()}
              accounts={accountsData()?.accounts ?? []}
              filepath={currentFile() ?? null}
              line={requestedLine()}
              onChange={handleValueChange}
              onSaveRequest={handleSaveRequest}
            />
          </Match>
        </Switch>
      </div>

      {/* Save success toast */}
      <Show when={saveToast.visible()}>
        <div class="toast toast-end">
          <div ref={saveToast.setToastRef} class="alert hidden alert-success">
            <span>File saved</span>
          </div>
        </div>
      </Show>

      {/* External file change toast - click to reload */}
      <Show when={fileChange.pendingReload()}>
        <div class="toast toast-end">
          <div
            ref={fileChange.setToastRef}
            class="alert hidden cursor-pointer alert-info"
            onClick={fileChange.handleReloadClick}
          >
            <span>File changed — click to reload</span>
          </div>
        </div>
      </Show>

      {/* Offline indicator */}
      <Show when={fileChange.connectionLost()}>
        <div class="toast toast-end toast-bottom">
          <div class="alert alert-warning">
            <span>Connection lost. Reconnecting...</span>
          </div>
        </div>
      </Show>

      {/* Conflict confirmation modal */}
      <Show when={showConflictModal()}>
        <dialog ref={(el) => (conflictModalRef = el)} class="modal">
          <div class="modal-box">
            <h3 class="text-lg font-bold">File Changed</h3>
            <p class="py-4">This file was modified externally. Overwrite with your changes?</p>
            <div class="modal-action">
              <button class="btn" onClick={handleCancelOverwrite}>
                Cancel
              </button>
              <button class="btn btn-warning" onClick={handleForceOverwrite}>
                Overwrite
              </button>
            </div>
          </div>
          <form method="dialog" class="modal-backdrop">
            <button onClick={handleCancelOverwrite}>close</button>
          </form>
        </dialog>
      </Show>
    </>
  );
};

export default Editor;
