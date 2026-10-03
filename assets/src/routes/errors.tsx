import { type Component, For, Match, Switch } from "solid-js";
import { A } from "@solidjs/router";
import { FinancialReport } from "../components/financial-report";
import { editorHref, errorMessage, ledgerErrors, relativePath } from "../lib/errors";

// Errors lists the ledger's errors, each with a link to its line in the
// editor.
const Errors: Component = () => {
  return (
    <FinancialReport.Root>
      <Switch>
        <Match when={ledgerErrors.loading && !ledgerErrors.latest}>
          <FinancialReport.Loading />
        </Match>
        <Match when={ledgerErrors.error as Error | undefined}>
          {(error) => <FinancialReport.Error error={error()} />}
        </Match>
        <Match when={ledgerErrors.latest?.errors.length === 0}>
          <FinancialReport.Empty>The ledger has no errors.</FinancialReport.Empty>
        </Match>
        <Match when={ledgerErrors.latest}>
          {(data) => (
            <ul class="flex flex-col gap-3" aria-label="Ledger errors">
              <For each={data().errors}>
                {(error) => (
                  <li class="rounded-box border border-base-300 bg-base-100 p-3 sm:p-4">
                    {error.position && (
                      <A
                        href={editorHref(error.position)}
                        class="link font-mono text-sm break-all link-primary"
                      >
                        {relativePath(error.position.filename, data().root)}:{error.position.line}
                      </A>
                    )}
                    <p class="mt-1 text-sm break-words whitespace-pre-wrap">
                      {errorMessage(error)}
                    </p>
                  </li>
                )}
              </For>
            </ul>
          )}
        </Match>
      </Switch>
    </FinancialReport.Root>
  );
};

export default Errors;
