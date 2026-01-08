export interface AccountInfo {
  name: string;
  type: string;
}

export interface EditorError {
  type: string;
  message: string;
  position?: {
    filename: string;
    line: number;
    column: number;
  };
}

export interface BalanceNode {
  name: string;
  account?: string;
  depth: number;
  balance: Record<string, string>;
  children?: BalanceNode[];
}

export interface BalancesResponse {
  roots: BalanceNode[];
  currencies: string[];
  startDate?: string;
  endDate?: string;
}
