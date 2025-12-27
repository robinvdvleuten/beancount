import type { EditorView as CodeMirrorEditorView } from "@uiw/react-codemirror";
import type { Diagnostic } from "@codemirror/lint";
import type { EditorError } from "../types";

export function errorsToDiagnostics(errors: EditorError[] | null, view: CodeMirrorEditorView): Diagnostic[] {
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
      // Start error marker at beginning of line for better visibility
      const from = line.from;
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
