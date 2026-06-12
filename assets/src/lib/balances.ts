import type { BalancesResponse } from "../types";

export const fetchBalances = async (types: string[]): Promise<BalancesResponse> => {
  const typeFilter = types.map(encodeURIComponent).join(",");
  const response = await fetch(`/api/balances?types=${typeFilter}`);

  if (!response.ok) {
    throw new Error(`Failed to fetch: ${response.statusText}`);
  }

  return (await response.json()) as BalancesResponse;
};
