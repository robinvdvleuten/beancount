import type { BalancesResponse } from "../types";

export interface Period {
  /** First day of the period, inclusive (YYYY-MM-DD). */
  startDate?: string;
  /** Last day of the period, inclusive; alone, the balance as of this day. */
  endDate?: string;
}

/**
 * How a report states its balances: "units", "cost", "market", or the
 * currency to convert to. Undefined is the API's default, at cost.
 */
export type Valuation = string | undefined;

export interface ValuationOption {
  /** The `?valuation=` value, undefined for the default (at cost). */
  value: Valuation;
  label: string;
}

// valuationOptions lists the Valuations a report offers, like fava's
// switch: Units, At cost, At market value, and Converted to each operating
// currency, in order.
export const valuationOptions = (operatingCurrencies: string[] = []): ValuationOption[] => [
  { value: "units", label: "Units" },
  { value: undefined, label: "At cost" },
  { value: "market", label: "At market value" },
  ...operatingCurrencies.map((currency) => ({
    value: currency,
    label: `Converted to ${currency}`,
  })),
];

// withValuation appends valuation to path as its ?valuation= parameter,
// leaving path alone at the default.
export const withValuation = (path: string, valuation: Valuation): string =>
  valuation ? `${path}?valuation=${encodeURIComponent(valuation)}` : path;

// fetchBalances fetches the balances of the given account types, or of every
// type when types is empty, over period (current balances when it is empty),
// stated under valuation (at cost when it is undefined).
export const fetchBalances = async (
  types: string[],
  period: Period = {},
  valuation?: Valuation,
): Promise<BalancesResponse> => {
  const params: string[] = [];
  if (types.length > 0) params.push(`types=${types.map(encodeURIComponent).join(",")}`);
  if (period.startDate) params.push(`startDate=${encodeURIComponent(period.startDate)}`);
  if (period.endDate) params.push(`endDate=${encodeURIComponent(period.endDate)}`);
  if (valuation) params.push(`valuation=${encodeURIComponent(valuation)}`);

  const query = params.length > 0 ? `?${params.join("&")}` : "";
  const response = await fetch(`/api/balances${query}`);

  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(`Failed to fetch: ${message || response.statusText}`);
  }

  return (await response.json()) as BalancesResponse;
};
