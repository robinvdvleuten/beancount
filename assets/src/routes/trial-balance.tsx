import { type Component, For, Match, Show, Switch, createResource } from "solid-js";
import { useFileChange } from "../hooks/useFileChange";
import { fetchBalances } from "../lib/balances";
import { FinancialReport } from "../components/financial-report";

const TrialBalance: Component = () => {
  // No types: every account type, rooted under the ledger's own names
  const [data, { refetch }] = createResource(() => fetchBalances([]));

  // File change detection via SSE - click to reload
  const fileChange = useFileChange({
    getLastFingerprint: () => undefined, // No fingerprint tracking needed
    onReload: () => {
      void refetch();
    },
  });

  return (
    <>
      <FinancialReport.Root>
        <Switch>
          <Match when={data.loading}>
            <FinancialReport.Loading />
          </Match>

          <Match when={data.error as Error | undefined}>
            {(error) => <FinancialReport.Error error={error()} />}
          </Match>

          <Match when={data()}>
            {(report) => (
              <Show
                when={report().roots.length > 0}
                fallback={<FinancialReport.Empty>No accounts found.</FinancialReport.Empty>}
              >
                <FinancialReport.Column>
                  <For each={report().roots}>
                    {(section) => (
                      <FinancialReport.Table
                        section={section}
                        currencies={report().currencies}
                        operatingCurrencies={report().operatingCurrencies}
                      />
                    )}
                  </For>
                </FinancialReport.Column>
              </Show>
            )}
          </Match>
        </Switch>
      </FinancialReport.Root>

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
    </>
  );
};

export default TrialBalance;
