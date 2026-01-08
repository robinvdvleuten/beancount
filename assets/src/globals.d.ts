declare global {
  interface Window {
    __metadata: {
      version: string;
      commitSHA: string;
      readOnly: boolean;
    };
    __files: {
      root: string;
      includes: string[];
    };
  }
}

declare module "virtual:globals" {
  export const meta: {
    version: string;
    commitSHA: string;
    readOnly: boolean;
  };
  export const files: {
    root: string;
    includes: string[];
  };
}
