declare global {
  interface Window {
    __metadata: {
      version: string;
      commitSHA: string;
      readOnly: boolean;
    };
  }
}

declare module 'virtual:globals' {
  export const meta: {
    version: string;
    commitSHA: string;
    readOnly: boolean;
  };
}
