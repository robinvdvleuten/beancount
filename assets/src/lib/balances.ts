import type { BalancesResponse } from "../types";

export interface Period {
  /** First day of the period, inclusive (YYYY-MM-DD). */
  startDate?: string;
  /** Last day of the period, inclusive; alone, the balance as of this day. */
  endDate?: string;
}

// fetchBalances fetches the balances of the given account types, or of every
// type when types is empty, over period (current balances when it is empty).
export const fetchBalances = async (
  types: string[],
  period: Period = {},
): Promise<BalancesResponse> => {
  const params: string[] = [];
  if (types.length > 0) params.push(`types=${types.map(encodeURIComponent).join(",")}`);
  if (period.startDate) params.push(`startDate=${encodeURIComponent(period.startDate)}`);
  if (period.endDate) params.push(`endDate=${encodeURIComponent(period.endDate)}`);

  const query = params.length > 0 ? `?${params.join("&")}` : "";
  const response = await fetch(`/api/balances${query}`);

  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(`Failed to fetch: ${message || response.statusText}`);
  }

  return (await response.json()) as BalancesResponse;
};
