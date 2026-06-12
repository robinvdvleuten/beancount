import { type Component, For, Match, Show, Switch, createResource } from "solid-js";
import { fetchBalances } from "../lib/balances";
import { FinancialReport } from "../components/financial-report";

const BalanceSheet: Component = () => {
  const [data] = createResource(() => fetchBalances(["Assets", "Liabilities", "Equity"]));
  const assetSections = () => FinancialReport.getSections(data()?.roots, ["Assets"]);
  const liabilityAndEquitySections = () =>
    FinancialReport.getSections(data()?.roots, ["Liabilities", "Equity"]);
  const hasRows = () => assetSections().length > 0 || liabilityAndEquitySections().length > 0;

  return (
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
                      <FinancialReport.Table section={section} currencies={report().currencies} />
                    )}
                  </For>
                </FinancialReport.Column>
                <FinancialReport.Column>
                  <For each={liabilityAndEquitySections()}>
                    {(section) => (
                      <FinancialReport.Table section={section} currencies={report().currencies} />
                    )}
                  </For>
                </FinancialReport.Column>
              </FinancialReport.Grid>
            </Show>
          )}
        </Match>
      </Switch>
    </FinancialReport.Root>
  );
};

export default BalanceSheet;
