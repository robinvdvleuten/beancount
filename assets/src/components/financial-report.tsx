import { type ParentComponent, For } from "solid-js";
import type { BalanceNode } from "../types";

interface FlatRow {
  name: string;
  account?: string;
  depth: number;
  balance: Record<string, string>;
  hasChildren: boolean;
}

interface ErrorProps {
  error: Error;
}

interface TableProps {
  section: BalanceNode;
  currencies: string[];
}

const flattenNode = (node: BalanceNode, depth = 0): FlatRow[] => {
  const row: FlatRow = {
    name: node.name,
    account: node.account,
    depth,
    balance: node.balance,
    hasChildren: (node.children?.length ?? 0) > 0,
  };

  if (!node.children || node.children.length === 0) {
    return [row];
  }

  return [row, ...node.children.flatMap((child) => flattenNode(child, depth + 1))];
};

const formatAmount = (amount: string | undefined): string => {
  if (!amount) return "--";

  const num = parseFloat(amount);
  return num.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
};

const displayName = (row: FlatRow): string => {
  if (!row.account) return row.name;

  return row.name.split(":").pop() ?? row.name;
};

const primaryCurrency = (currencies: string[]): string | undefined =>
  currencies.includes("USD") ? "USD" : currencies[0];

const otherCurrencies = (currencies: string[], primary: string | undefined): string[] =>
  currencies.filter((currency) => currency !== primary);

const formatAmountWithCurrency = (amount: string | undefined, currency: string): string =>
  `${formatAmount(amount)} ${currency}`;

const valueClass = (row: FlatRow): string =>
  row.hasChildren ? "text-base-content/60" : "text-base-content";

const getSections = (roots: BalanceNode[] | undefined, sectionNames: string[]): BalanceNode[] =>
  sectionNames
    .map((sectionName) => roots?.find((root) => root.name === sectionName))
    .filter((section): section is BalanceNode => section !== undefined);

const Root: ParentComponent = (props) => (
  <div class="flex-1 overflow-auto p-4">{props.children}</div>
);

const Loading = () => (
  <div class="flex items-center justify-center py-12">
    <span class="loading loading-spinner loading-lg" />
  </div>
);

const Error = (props: ErrorProps) => (
  <div class="alert alert-error" role="alert">
    <span>Error: {props.error.message}</span>
  </div>
);

const Empty: ParentComponent = (props) => (
  <div class="py-12 text-center text-base-content/50">{props.children}</div>
);

const Grid: ParentComponent = (props) => (
  <div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">{props.children}</div>
);

const Column: ParentComponent = (props) => <div class="flex flex-col gap-4">{props.children}</div>;

const Table = (props: TableProps) => {
  const primary = () => primaryCurrency(props.currencies);
  const secondary = () => otherCurrencies(props.currencies, primary());

  return (
    <div class="overflow-x-auto">
      <table class="table table-sm" aria-label={props.section.name}>
        <thead>
          <tr class="bg-base-200">
            <th aria-label="Account" />
            <th class="text-right font-mono">{primary()}</th>
            <th class="text-right font-mono">Other</th>
          </tr>
        </thead>
        <tbody>
          <For each={flattenNode(props.section)}>
            {(row) => (
              <tr>
                <td
                  class="text-primary"
                  style={{
                    "padding-left": `${row.depth * 1.25 + 0.75}rem`,
                  }}
                >
                  {displayName(row)}
                </td>
                <td class={`align-top text-right font-mono tabular-nums ${valueClass(row)}`}>
                  {formatAmount(primary() ? row.balance[primary() as string] : undefined)}
                </td>
                <td class={`align-top text-right font-mono tabular-nums ${valueClass(row)}`}>
                  <For each={secondary().filter((currency) => row.balance[currency])}>
                    {(currency) => (
                      <div>{formatAmountWithCurrency(row.balance[currency], currency)}</div>
                    )}
                  </For>
                </td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </div>
  );
};

export const FinancialReport = {
  Root,
  Loading,
  Error,
  Empty,
  Grid,
  Column,
  Table,
  getSections,
};
