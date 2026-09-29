import type { BalancesResponse } from "../types";

// fetchBalances fetches the balances of the given account types, or of every
// type when types is empty.
export const fetchBalances = async (types: string[]): Promise<BalancesResponse> => {
  const query = types.length > 0 ? `?types=${types.map(encodeURIComponent).join(",")}` : "";
  const response = await fetch(`/api/balances${query}`);

  if (!response.ok) {
    throw new Error(`Failed to fetch: ${response.statusText}`);
  }

  return (await response.json()) as BalancesResponse;
};
