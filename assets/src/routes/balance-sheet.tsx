import { type Component, For, Match, Show, Switch, createResource } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { useFileChange } from "../hooks/useFileChange";
import { fetchBalances } from "../lib/balances";
import { FinancialReport } from "../components/financial-report";

const BalanceSheet: Component = () => {
  // The as-of date lives in the URL (?asOf=), so reloading or sharing keeps it
  const [searchParams, setSearchParams] = useSearchParams<{ asOf: string; valuation: string }>();
  const [data, { refetch }] = createResource(
    () => ({ period: { endDate: searchParams.asOf }, valuation: searchParams.valuation }),
    ({ period, valuation }) =>
      fetchBalances(["Assets", "Liabilities", "Equity"], period, valuation, true),
  );

  // The picker offers each operating currency once a report has listed them
  const operatingCurrencies = () => (data.error ? undefined : data.latest?.operatingCurrencies);

  // File change detection via SSE - click to reload
  const fileChange = useFileChange({
    getLastFingerprint: () => undefined, // No fingerprint tracking needed
    onReload: () => {
      void refetch();
    },
  });
  const assetSections = () => FinancialReport.getSections(data()?.roots, ["Assets"]);
  const liabilityAndEquitySections = () =>
    FinancialReport.getSections(data()?.roots, ["Liabilities", "Equity"]);
  const hasRows = () => assetSections().length > 0 || liabilityAndEquitySections().length > 0;

  return (
    <>
      <FinancialReport.Root>
        <FinancialReport.Toolbar>
          <FinancialReport.DateField
            label="As of"
            value={searchParams.asOf}
            onChange={(asOf) => setSearchParams({ asOf })}
          />
          <Show when={searchParams.asOf}>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              onClick={() => setSearchParams({ asOf: undefined })}
            >
              Today
            </button>
          </Show>
          <FinancialReport.ValuationField
            value={searchParams.valuation}
            operatingCurrencies={operatingCurrencies()}
            onChange={(valuation) => setSearchParams({ valuation })}
          />
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
                    No assets, liabilities, or equity accounts found.
                  </FinancialReport.Empty>
                }
              >
                <FinancialReport.Grid>
                  <FinancialReport.Column>
                    <For each={assetSections()}>
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
                    <For each={liabilityAndEquitySections()}>
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

export default BalanceSheet;
