# Import checks for beancount.* plugin names outside beancount.plugins

`check` reports a `plugin` name as `Error importing` only when it sits under
`beancount.plugins.` and v2 doesn't ship it. Other names under `beancount.`
(`beancount.nope`, `beancount.ops.nope`) stay unchecked, although bean-check
fails to import them. We don't plan to close that gap.

## Why this is out of scope

Reporting them would mean embedding v2's full module list (every
`beancount/**/*.py` and package, well over a hundred names) and keeping it
in step with the version we target, for a check that only ever yields an
error. The mistake it catches is rare: the likely typo, a misspelled plugin
under `beancount.plugins.`, is already reported, and a plugin name that
doesn't import can't lose data, it just doesn't run.

Full parity would not stop at the error either. Naming one of v2's default
plugins runs it a second time there (probed against bean-check 2.3.6):

```beancount
plugin "beancount.ops.pad"      ; every used pad becomes "Unused Pad entry"
plugin "beancount.ops.balance"  ; every failing balance is reported twice
```

Copying that is quirk-matching nobody depends on. Existing non-plugin
modules (`beancount.core.data`, `beancount.ops.documents`, `beancount`)
import and do nothing in v2, which matches what we do.

Both behaviours are recorded under "Other Built-in Plugins" in
`testdata/compliance/KNOWN_GAPS.md`.

Reopen this if users hit it in practice, for example a misspelled
`beancount.plugin.` prefix that silently disabled a Built-in Plugin.

## Prior requests

- #480: "check: unknown beancount.* modules outside beancount.plugins aren't reported"
