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
