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

interface DateFieldProps {
  label: string;
  value: string | undefined;
  onChange: (value: string | undefined) => void;
}

interface TableProps {
  section: BalanceNode;
  currencies: string[];
  operatingCurrencies?: string[];
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

  const sign = amount.startsWith("-") ? "-" : "";
  const unsigned = sign ? amount.slice(1) : amount;
  const [integer = "0", fraction = ""] = unsigned.split(".");
  const groupedInteger = integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",");

  if (fraction.length === 0) {
    return `${sign}${groupedInteger}.00`;
  }

  if (fraction.length === 1) {
    return `${sign}${groupedInteger}.${fraction}0`;
  }

  return `${sign}${groupedInteger}.${fraction}`;
};

const displayName = (row: FlatRow): string => {
  if (!row.account) return row.name;

  return row.name.split(":").pop() ?? row.name;
};

// Like fava, each operating currency gets a column of its own; without any,
// the ledger's USD or else its first currency does.
const mainCurrencies = (currencies: string[], operating: string[] = []): string[] => {
  if (operating.length > 0) return operating;

  const fallback = currencies.includes("USD") ? "USD" : currencies[0];
  return fallback ? [fallback] : [];
};

const otherCurrencies = (currencies: string[], main: string[]): string[] =>
  currencies.filter((currency) => !main.includes(currency));

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
    <span class="loading loading-lg loading-spinner" />
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

const Toolbar: ParentComponent = (props) => (
  <div class="mb-4 flex flex-wrap items-end gap-3">{props.children}</div>
);

// DateField edits one YYYY-MM-DD date; clearing it reports undefined.
const DateField = (props: DateFieldProps) => (
  <label class="flex flex-col gap-1 text-xs text-base-content/70">
    {props.label}
    <input
      type="date"
      class="input input-sm"
      value={props.value ?? ""}
      onChange={(event) => props.onChange(event.currentTarget.value || undefined)}
    />
  </label>
);

const Table = (props: TableProps) => {
  const main = () => mainCurrencies(props.currencies, props.operatingCurrencies);
  const secondary = () => otherCurrencies(props.currencies, main());

  return (
    <div class="overflow-x-auto">
      <table class="table table-sm" aria-label={props.section.name}>
        <thead>
          <tr class="bg-base-200">
            <th aria-label="Account" />
            <For each={main()}>
              {(currency) => <th class="text-right font-mono">{currency}</th>}
            </For>
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
                <For each={main()}>
                  {(currency) => (
                    <td class={`text-right align-top font-mono tabular-nums ${valueClass(row)}`}>
                      {formatAmount(row.balance[currency])}
                    </td>
                  )}
                </For>
                <td class={`text-right align-top font-mono tabular-nums ${valueClass(row)}`}>
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
  Toolbar,
  DateField,
  Table,
  getSections,
};
