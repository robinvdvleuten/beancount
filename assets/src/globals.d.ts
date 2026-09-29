declare global {
  interface Window {
    __metadata: {
      version: string;
      commitSHA: string;
      readOnly: boolean;
      watching: boolean;
      title: string;
    };
  }
}

declare module "virtual:globals" {
  export const meta: {
    version: string;
    commitSHA: string;
    readOnly: boolean;
    watching: boolean;
    title: string;
  };
}
