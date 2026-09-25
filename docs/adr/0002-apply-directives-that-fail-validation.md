# Directives that fail validation are still applied

Validate reports every error it finds and, separately, returns the delta to apply, or none; Apply runs whenever there is a delta, errors or not. Beancount books every transaction before it checks it, so an unbalanced transaction, a posting to an unopened or closed account, a disallowed currency or a negative cost is reported and still counts, and an `open` with an invalid booking method still opens the account. Only a transaction that cannot be booked (a malformed number, missing numbers that cannot be interpolated, a reduction that matches no lot or several) is a Dropped transaction, left out of the ledger and the processed AST. We follow that split so one typo produces one error instead of a chain of follow-on errors in unrelated directives, and so balances and queries match `bean-check` and `bean-query`.

## Considered options

- **Apply only after a clean validation** (the old contract). Simple, but every later directive that depends on the failed one reports its own error, and balances diverge from beancount's.
- **A fatal or non-fatal flag on each error type**, applying when no fatal error was returned. We rejected it: it spreads the decision over every error type, and a flag and the delta could disagree. The validator that builds the delta already knows whether it got far enough to build one.

## Consequences

- Apply can no longer assume the directive it applies is valid. A posting to an account that is not open yet is held until that account opens (never, if it never does), since there is no account state to change.
- A handler must return a nil delta, not a nil pointer inside a non-nil interface, when nothing may be applied.
