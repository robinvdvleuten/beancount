import { test, expect } from "@playwright/test";

async function navigateToBalanceSheet(page: import("@playwright/test").Page) {
  const balancesLoaded = page.waitForResponse(
    (response) =>
      response.url().includes("/api/balances?types=Assets,Liabilities,Equity") && response.ok(),
  );

  await page.goto("/balance-sheet");
  await balancesLoaded;
}

async function waitForBalanceSheetRows(page: import("@playwright/test").Page) {
  const assetsTable = page.getByRole("table", { name: "Assets" });
  await expect(assetsTable).toBeVisible();
  await expect(assetsTable.getByRole("columnheader", { name: "Account" })).toBeVisible();
  await expect.poll(async () => await page.locator("tbody tr").count()).toBeGreaterThan(0);
}

async function expectBalanceSheetRow(
  page: import("@playwright/test").Page,
  tableName: string,
  accountName: string,
  expectedTexts: string[] = [],
) {
  const row = page
    .getByRole("table", { name: tableName })
    .locator("tbody tr")
    .filter({
      has: page.getByRole("cell", { name: accountName, exact: true }),
    })
    .first();

  await expect(row).toBeVisible();
  for (const text of expectedTexts) {
    await expect(row).toContainText(text);
  }
}

test.describe("Balance Sheet", () => {
  test("renders page with header and navigation item", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await navigateToBalanceSheet(page);

    await expect(page.getByRole("heading", { name: "Balance Sheet" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Balance Sheet" })).toBeVisible();
    await waitForBalanceSheetRows(page);

    expect(errors).toEqual([]);
  });

  test("displays balance sheet sections in separate tables", async ({ page }) => {
    await navigateToBalanceSheet(page);
    await waitForBalanceSheetRows(page);

    await expect(page.getByRole("table")).toHaveCount(3);
    await expectBalanceSheetRow(page, "Assets", "Assets");
    await expectBalanceSheetRow(page, "Liabilities", "Liabilities");
    await expectBalanceSheetRow(page, "Equity", "Equity");
  });

  test("displays representative account rows", async ({ page }) => {
    await navigateToBalanceSheet(page);
    await waitForBalanceSheetRows(page);

    await expectBalanceSheetRow(page, "Assets", "BofA");
    await expectBalanceSheetRow(page, "Liabilities", "AccountsPayable");
    await expectBalanceSheetRow(page, "Equity", "Opening-Balances");
  });

  test("displays multicurrency columns with amounts", async ({ page }) => {
    await navigateToBalanceSheet(page);
    await waitForBalanceSheetRows(page);

    const assetsTable = page.getByRole("table", { name: "Assets" });
    await expect(assetsTable.getByRole("columnheader", { name: "USD", exact: true })).toBeVisible();
    await expect(assetsTable.getByRole("columnheader", { name: "Other" })).toBeVisible();

    await expectBalanceSheetRow(page, "Assets", "Assets");
  });

  test("shows empty state when API returns no rows", async ({ page }) => {
    await page.route("**/api/balances**", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          roots: [],
          currencies: [],
        }),
      }),
    );

    await page.goto("/balance-sheet", { waitUntil: "networkidle" });

    await expect(page.getByText("No assets, liabilities, or equity accounts found.")).toBeVisible();
    await expect(page.locator("tbody tr")).toHaveCount(0);
  });

  test("shows error state when API fails", async ({ page }) => {
    await page.route("**/api/balances**", (route) =>
      route.fulfill({
        status: 500,
        contentType: "text/plain",
        body: "Internal Server Error",
      }),
    );

    await page.goto("/balance-sheet", { waitUntil: "networkidle" });

    const errorAlert = page.getByRole("alert");
    await expect(errorAlert).toBeVisible();
    await expect(errorAlert).toContainText("Error:");
  });
});
