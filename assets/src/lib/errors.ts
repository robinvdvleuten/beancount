import { createResource, createRoot } from "solid-js";
import type { EditorError } from "../types";

interface LedgerErrorsResponse {
  errors: EditorError[] | null;
  files: { root: string; includes: string[] };
}

export interface LedgerErrors {
  errors: EditorError[];
  /** The root file's path, which the errors' files are shown relative to */
  root: string;
}

const fetchLedgerErrors = async (): Promise<LedgerErrors> => {
  const response = await fetch("/api/source");
  if (!response.ok) {
    throw new Error(`Failed to fetch ledger errors: ${response.statusText}`);
  }
  const data = (await response.json()) as LedgerErrorsResponse;
  return { errors: data.errors ?? [], root: data.files.root };
};

// One store of the ledger's errors for every page, so the sidebar's count
// and the errors page agree, and a save in the editor refreshes both.
const store = createRoot(() => createResource(fetchLedgerErrors));

/** The ledger's load and validation errors, as the web API reports them. */
export const ledgerErrors = store[0];

/** Fetches the ledger's errors again, after a save or a reload. */
export const refetchLedgerErrors = () => void store[1].refetch();

/** A file's path relative to the root file's directory, as the editor shows it. */
export const relativePath = (filepath: string, root: string): string => {
  const directory = root.slice(0, root.lastIndexOf("/") + 1);
  return filepath.startsWith(directory) ? filepath.slice(directory.length) : filepath;
};

/** An error's message without the "path:line:" its position already gives. */
export const errorMessage = (error: EditorError): string => {
  const position = error.position;
  if (!position) return error.message;
  const prefix = `${position.filename}:${position.line}:`;
  if (!error.message.startsWith(prefix)) return error.message;
  // A syntax error's position also names its column
  return error.message.slice(prefix.length).replace(/^\d+:/, "").trimStart();
};

/** The editor's address opened at an error's file and line. */
export const editorHref = (position: { filename: string; line: number }): string =>
  `/editor?${new URLSearchParams({ file: position.filename, line: String(position.line) })}`;
