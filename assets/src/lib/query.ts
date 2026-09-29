export type QueryFormat = "text" | "csv";

// runQuery runs one BQL statement on the server and returns what
// `beancount query` prints for it, bean-query's error messages included.
export const runQuery = async (query: string, format: QueryFormat): Promise<string> => {
  const response = await fetch("/api/query", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query, format }),
  });

  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(`Failed to run query: ${message || response.statusText}`);
  }

  return ((await response.json()) as { output: string }).output;
};
