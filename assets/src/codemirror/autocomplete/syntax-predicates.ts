import type { CompletionContext } from "@codemirror/autocomplete";
import type { SyntaxNode } from "@lezer/common";

// Account directives that expect account names as arguments
export const ACCOUNT_DIRECTIVE_TYPES = new Set([
  "Open",
  "Close",
  "Balance",
  "Pad",
  "Note",
  "Document",
]);

/**
 * Checks if the cursor is inside a string literal, line comment, or org-mode header.
 * Walks up the syntax tree from the current node to check if any ancestor
 * is a String, LineComment, or OrgHeader node.
 *
 * @param node - The syntax node at cursor position
 * @returns true if inside string/comment/org header context, false otherwise
 */
export const isInStringOrComment = (node: SyntaxNode): boolean => {
  let current: SyntaxNode | null = node;
  while (current) {
    if (
      current.name === "String" ||
      current.name === "LineComment" ||
      current.name === "OrgHeader"
    ) {
      return true;
    }
    current = current.parent;
  }
  return false;
};

/**
 * Checks if the cursor is inside a posting line (indented transaction line).
 * Stops at Transaction boundaries to ensure we only match inside Posting children,
 * not in the transaction's payee or narration strings.
 *
 * @param node - The syntax node at cursor position
 * @returns true if inside a Posting node, false otherwise
 */
export const isChildOfPosting = (node: SyntaxNode): boolean => {
  let current: SyntaxNode | null = node;
  while (current) {
    // Check for Posting first before Transaction check
    if (current.name === "Posting") {
      return true;
    }
    // Stop at Transaction boundary - postings are children of transactions,
    // so if we hit Transaction without finding a Posting, we're in the
    // transaction header (payee/narration), not in a posting
    if (current.name === "Transaction") {
      return false;
    }
    current = current.parent;
  }
  return false;
};

/**
 * Checks if the cursor is positioned after an account directive keyword,
 * specifically in the position where an account name should appear.
 * Verifies that cursor is after the date (second argument position).
 *
 * @param context - The completion context with cursor position
 * @param node - The syntax node at cursor position
 * @returns true if in account position of a directive, false otherwise
 */
export const isAfterAccountDirectiveKeyword = (
  context: CompletionContext,
  node: SyntaxNode,
): boolean => {
  let current: SyntaxNode | null = node;
  while (current) {
    // Check if we're in an account directive
    if (ACCOUNT_DIRECTIVE_TYPES.has(current.name)) {
      // For these directives, the account comes after the date
      // Find the Date node within this directive to ensure we're in the right position
      let child = current.firstChild;
      while (child) {
        if (child.name === "Date") {
          // Check if cursor is after the date (in account position)
          if (context.pos > child.to) {
            return true;
          }
          return false;
        }
        child = child.nextSibling;
      }
      // If no date found but we're in the directive, allow completion
      // (handles incomplete directives like "2024-01-01 open ")
      return true;
    }
    current = current.parent;
  }
  return false;
};
