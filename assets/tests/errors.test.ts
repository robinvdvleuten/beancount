import { test, expect, type Page } from "@playwright/test";

/**
 * Ledger errors tests.
 *
 * Verifies that every page shows whether the ledger has errors, that the
 * errors page lists them, and that each error opens the editor at its line.
 */

interface SourceResponse {
  source: string;
  errors: Array<{ message: string; position?: { filename: string; line: number } }> | null;
  files: { root: string };
}

async function getSource(page: Page) {
  const response = await page.request.get("/api/source");
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as SourceResponse;
}

async function putSource(page: Page, source: string) {
  const response = await page.request.put("/api/source", { data: { source } });
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as SourceResponse;
}

test.describe("Ledger errors", () => {
  test("shows no count for a ledger without errors", async ({ page }) => {
    const { errors } = await getSource(page);
    expect(errors ?? []).toHaveLength(0);

    await page.goto("/income-statement", { waitUntil: "networkidle" });
    await expect(page.getByRole("link", { name: /^Errors/ })).toHaveCount(0);
  });

  test("lists each error and opens the editor at its line", async ({ page }) => {
    const { source: originalSource } = await getSource(page);
    // A failing balance assertion, the last line of the ledger
    const failing = "2099-01-01 balance Assets:US:BofA:Checking 1.00 USD";
    const failingLine = originalSource.trimEnd().split("\n").length + 1;

    try {
      const saved = await putSource(page, `${originalSource.trimEnd()}\n${failing}\n`);
      expect(saved.errors).toHaveLength(1);

      // Every page shows the count
      await page.goto("/balance-sheet", { waitUntil: "networkidle" });
      const count = page.getByRole("link", { name: "Errors (1)" });
      await expect(count).toBeVisible();

      // The errors page lists the error with its file and line
      await count.click();
      await page.waitForURL("/errors");
      const list = page.getByRole("list", { name: "Ledger errors" });
      await expect(list.getByRole("listitem")).toHaveCount(1);
      const location = list.getByRole("link", { name: `example.beancount:${failingLine}` });
      await expect(location).toBeVisible();

      // Following it opens the editor with that line in view and selected
      await location.click();
      await page.waitForURL(/\/editor\?/);
      const url = new URL(page.url());
      expect(url.searchParams.get("file")).toBe(saved.files.root);
      expect(url.searchParams.get("line")).toBe(String(failingLine));

      const line = page.locator(".cm-line", { hasText: failing });
      await expect(line).toBeInViewport();
      await expect.poll(() => page.evaluate("String(getSelection())")).toBe(failing);
    } finally {
      await putSource(page, originalSource);
    }
  });
});
