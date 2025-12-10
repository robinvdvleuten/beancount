import { autocompletion, type CompletionContext } from "@codemirror/autocomplete";
import { syntaxTree } from "@codemirror/language";
import { matchSorter } from "match-sorter";
import type { AccountInfo } from "../../types";
import { isInStringOrComment, isChildOfPosting, isAfterAccountDirectiveKeyword } from "./syntax-predicates";

/**
 * Main context detection function that determines if autocomplete should trigger.
 * Checks if the cursor is in a valid position for account name completion.
 *
 * @param context - The completion context with editor state and cursor position
 * @returns true if autocomplete should show account suggestions, false otherwise
 */
export const isInAccountContext = (context: CompletionContext): boolean => {
  const tree = syntaxTree(context.state);
  if (!tree) {
    return false;
  }

  const node = tree.resolveInner(context.pos, -1);
  if (!node) {
    return false;
  }

  // Reject if inside string or comment
  if (isInStringOrComment(node)) {
    return false;
  }

  // Accept if we're on an Account node or inside a posting
  if (node.name === "Account" || isChildOfPosting(node)) {
    return true;
  }

  // Accept if we're after an account directive keyword
  if (isAfterAccountDirectiveKeyword(context, node)) {
    return true;
  }

  return false;
};

// Autocomplete function for account names using match-sorter for intelligent ranking
export const createAccountCompletion = (accounts: AccountInfo[]) => {
  return autocompletion({
    activateOnTyping: true,
    override: [
      (context: CompletionContext) => {
        // Only show completions when in valid account context
        if (!isInAccountContext(context)) {
          return null;
        }

        const word = context.matchBefore(/[\w:-]+/);

        // Only show completions when user is typing
        if (!word || (word.from === word.to && !context.explicit)) {
          return null;
        }

        // Use match-sorter with keys option to sort by account name
        const matchedAccounts = matchSorter(accounts, word.text, {
          keys: ["name"],
          threshold: matchSorter.rankings.CONTAINS,
        });

        const options = matchedAccounts.map((account) => ({
          label: account.name,
          type: "variable",
          detail: account.type,  // Show account type as detail
          apply: account.name,
        }));

        return {
          from: word.from,
          options,
          validFor: /^[\w:-]*$/,  // Keep completion open while typing valid account characters
        };
      },
    ],
  });
};
