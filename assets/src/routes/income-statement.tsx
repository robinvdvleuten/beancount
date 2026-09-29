import { type Component, For, Match, Show, Switch, createResource } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { useFileChange } from "../hooks/useFileChange";
import { fetchBalances } from "../lib/balances";
import { FinancialReport } from "../components/financial-report";

const IncomeStatement: Component = () => {
  // The period lives in the URL (?from=&to=), so reloading or sharing keeps it
  const [searchParams, setSearchParams] = useSearchParams<{ from: string; to: string }>();
  const [data, { refetch }] = createResource(
    () => ({ startDate: searchParams.from, endDate: searchParams.to }),
    (period) => fetchBalances(["Income", "Expenses"], period),
  );

  // File change detection via SSE - click to reload
  const fileChange = useFileChange({
    getLastFingerprint: () => undefined, // No fingerprint tracking needed
    onReload: () => {
      void refetch();
    },
  });

  const incomeSections = () => FinancialReport.getSections(data()?.roots, ["Income"]);
  const expenseSections = () => FinancialReport.getSections(data()?.roots, ["Expenses"]);
  const hasRows = () => incomeSections().length > 0 || expenseSections().length > 0;

  return (
    <>
      <FinancialReport.Root>
        <FinancialReport.Toolbar>
          <FinancialReport.DateField
            label="From"
            value={searchParams.from}
            onChange={(from) => setSearchParams({ from })}
          />
          <FinancialReport.DateField
            label="To"
            value={searchParams.to}
            onChange={(to) => setSearchParams({ to })}
          />
          <Show when={searchParams.from || searchParams.to}>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              onClick={() => setSearchParams({ from: undefined, to: undefined })}
            >
              All time
            </button>
          </Show>
        </FinancialReport.Toolbar>
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
                when={hasRows()}
                fallback={
                  <FinancialReport.Empty>
                    No income or expense transactions found.
                  </FinancialReport.Empty>
                }
              >
                <FinancialReport.Grid>
                  <FinancialReport.Column>
                    <For each={incomeSections()}>
                      {(section) => (
                        <FinancialReport.Table
                          section={section}
                          currencies={report().currencies}
                          operatingCurrencies={report().operatingCurrencies}
                        />
                      )}
                    </For>
                  </FinancialReport.Column>
                  <FinancialReport.Column>
                    <For each={expenseSections()}>
                      {(section) => (
                        <FinancialReport.Table
                          section={section}
                          currencies={report().currencies}
                          operatingCurrencies={report().operatingCurrencies}
                        />
                      )}
                    </For>
                  </FinancialReport.Column>
                </FinancialReport.Grid>
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

export default IncomeStatement;
