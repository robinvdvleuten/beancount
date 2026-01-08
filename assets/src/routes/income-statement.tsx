import {
  type Component,
  createResource,
  Show,
  For,
  Switch,
  Match,
} from "solid-js";
import DocumentCurrencyDollarIcon from "heroicons/24/solid/document-currency-dollar.svg?component-solid";
import type { BalancesResponse, BalanceNode } from "../types";

const fetchIncomeStatement = async (): Promise<BalancesResponse> => {
  const response = await fetch("/api/balances?types=Income,Expenses");
  if (!response.ok) {
    throw new Error(`Failed to fetch: ${response.statusText}`);
  }
  return (await response.json()) as BalancesResponse;
};

interface FlatRow {
  name: string;
  account?: string;
  depth: number;
  balance: Record<string, string>;
  isHeader: boolean;
}

const flattenNode = (node: BalanceNode, depth = 0): FlatRow[] => {
  const row: FlatRow = {
    name: node.name,
    account: node.account,
    depth,
    balance: node.balance,
    isHeader: !node.account,
  };

  if (!node.children || node.children.length === 0) {
    return [row];
  }

  return [
    row,
    ...node.children.flatMap((child) => flattenNode(child, depth + 1)),
  ];
};

const formatAmount = (amount: string | undefined): string => {
  if (!amount) return "—";
  const num = parseFloat(amount);
  return num.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
};

const IncomeStatement: Component = () => {
  const [data] = createResource(fetchIncomeStatement);

  const rows = () => {
    const d = data();
    if (!d) return [];
    return d.roots.flatMap((root) => flattenNode(root));
  };

  const currencies = () => data()?.currencies ?? [];

  return (
    <>
      <header class="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div class="flex items-center gap-3">
          <div class="text-primary">
            <DocumentCurrencyDollarIcon class="size-8" />
          </div>
          <div class="text-base-content">
            <h1 class="text-xl font-semibold">Income Statement</h1>
            <p class="text-sm text-base-content/50">All time</p>
          </div>
        </div>
      </header>

      <div class="flex-1 overflow-auto p-6">
        <Switch>
          <Match when={data.loading}>
            <div class="flex items-center justify-center py-12">
              <span class="loading loading-spinner loading-lg" />
            </div>
          </Match>

          <Match when={data.error as Error | undefined}>
            {(error) => (
              <div class="alert alert-error" role="alert">
                <span>Error: {error().message}</span>
              </div>
            )}
          </Match>

          <Match when={data()}>
            <div class="overflow-x-auto">
              <table class="table">
                <thead>
                  <tr>
                    <th>Account</th>
                    <For each={currencies()}>
                      {(currency) => <th class="text-right">{currency}</th>}
                    </For>
                  </tr>
                </thead>
                <tbody>
                  <For each={rows()}>
                    {(row) => (
                      <tr
                        class={row.isHeader ? "font-semibold bg-base-200" : ""}
                      >
                        <td
                          style={{
                            "padding-left": `${row.depth * 1.5 + 1}rem`,
                          }}
                        >
                          {row.isHeader ? row.name : row.name.split(":").pop()}
                        </td>
                        <For each={currencies()}>
                          {(currency) => (
                            <td class="text-right font-mono">
                              {formatAmount(row.balance[currency])}
                            </td>
                          )}
                        </For>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>

            <Show when={rows().length === 0}>
              <div class="text-center py-12 text-base-content/50">
                No income or expense transactions found.
              </div>
            </Show>
          </Match>
        </Switch>
      </div>
    </>
  );
};

export default IncomeStatement;
