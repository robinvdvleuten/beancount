# Typed deltas and a shared validator in the handler registry

The ledger's handler registry keeps its current shape: each `Handler` returns
its delta as `any` from `Validate` and type-asserts it in `Apply`, and each
`Validate` builds its own `validator` with `newValidator(l.accounts, l.config)`.
We don't plan to type the deltas through a generic adapter, and we don't plan
to hold one shared validator on the `Ledger`.

## Why this is out of scope

Thin handlers are the point of the design. AGENTS.md makes registry dispatch a
convention: a handler routes a directive kind to the functions that validate
and apply it, and the logic lives in those functions (validation, the Pad
state in `l.pads`, `applyTransaction`). A handler that is one or two calls
long is doing its job.

The two changes this was once proposed for cost more than they save:

- **Typed deltas.** Each `Apply` asserts only the delta its own `Validate`
  returned, so every run of the test suite exercises each assertion, and a
  mismatch panics right away in tests rather than misbehaving quietly. A
  generic adapter such as `typedHandler[D]` would add a layer of generics to
  remove about nine one-line assertions. The deletion test says this moves
  code rather than concentrating it.
- **One validator on the Ledger.** `validator` is a struct with two fields,
  `accounts` and `config`. Building one per directive is one small allocation,
  with no measured cost and no bug behind it. Keeping one on the `Ledger`
  would also mean `Validate` holds state that has to stay in step with
  `l.accounts`, which the current code gets for free.

Before #462, the Balance and Pad handlers carried real pad logic, which made
the registry look heavier than it is. That logic now lives in the Pad state
and `applyPadding`, and what remains is the thin dispatch the convention
intends.

Reopen this only if something concrete changes: a delta mix-up that tests
missed, a profile that shows `newValidator` costing something, or handlers
growing logic again.

## Prior requests

- #467: "ledger: typed deltas and one validator behind the handler registry"
