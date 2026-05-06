import { test, expect } from "@playwright/test";

/**
 * Income Statement page tests.
 *
 * Verifies the core functionality of the Income Statement page:
 * - Page loading and rendering
 * - API data fetching and display
 * - Table structure and content
 */

async function navigateToIncomeStatement(page: import("@playwright/test").Page) {
  const balancesLoaded = page.waitForResponse(
    (response) => response.url().includes("/api/balances?types=Income,Expenses") && response.ok(),
  );

  await page.goto("/income-statement");
  await balancesLoaded;
}

async function waitForIncomeStatementRows(page: import("@playwright/test").Page) {
  await expect(page.getByRole("table")).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Account" })).toBeVisible();
  await expect.poll(async () => await page.locator("tbody tr").count()).toBeGreaterThan(0);
}

async function expectIncomeStatementRow(
  page: import("@playwright/test").Page,
  accountName: string,
  expectedTexts: string[],
) {
  const row = page
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

test.describe("Income Statement", () => {
  test("renders page with header", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await navigateToIncomeStatement(page);

    await expect(page.getByRole("heading", { name: "Income Statement" })).toBeVisible();
    await waitForIncomeStatementRows(page);

    expect(errors).toEqual([]);
  });

  test("displays table with account data", async ({ page }) => {
    await navigateToIncomeStatement(page);
    await waitForIncomeStatementRows(page);

    await expectIncomeStatementRow(page, "Rent", ["74,400.00"]);
    await expectIncomeStatementRow(page, "Match401k", ["-27,500.00"]);
  });

  test("displays Income and Expenses sections", async ({ page }) => {
    await navigateToIncomeStatement(page);
    await waitForIncomeStatementRows(page);

    // Verify Income and Expenses section headers exist
    await expect(page.getByRole("cell", { name: "Income", exact: true })).toBeVisible();
    await expect(page.getByRole("cell", { name: "Expenses", exact: true })).toBeVisible();
  });

  test("displays currency columns with amounts", async ({ page }) => {
    await navigateToIncomeStatement(page);
    await waitForIncomeStatementRows(page);

    await expect(page.getByRole("columnheader", { name: "USD", exact: true })).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "VACHR", exact: true })).toBeVisible();

    await expectIncomeStatementRow(page, "Expenses", ["102,101.57", "184.00"]);
  });

  test("shows loading state initially", async ({ page }) => {
    // Slow down network to catch loading state
    await page.route("**/api/balances**", async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 500));
      await route.continue();
    });

    await page.goto("/income-statement");

    // Verify loading spinner is visible
    const loadingSpinner = page.locator(".loading-spinner");
    await expect(loadingSpinner).toBeVisible();

    // Wait for data to load
    await page.waitForLoadState("networkidle");

    // Verify loading spinner is gone and table is visible
    await expect(loadingSpinner).not.toBeVisible();
    await expect(page.getByRole("table")).toBeVisible();
  });

  test("displays hierarchical account structure", async ({ page }) => {
    await navigateToIncomeStatement(page);
    await waitForIncomeStatementRows(page);

    await expect(page.getByRole("cell", { name: "Income", exact: true })).toBeVisible();
    await expect(page.getByRole("cell", { name: "Expenses", exact: true })).toBeVisible();

    await expectIncomeStatementRow(page, "Home", ["80,760.97"]);
    await expectIncomeStatementRow(page, "Rent", ["74,400.00"]);
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

    await page.goto("/income-statement", { waitUntil: "networkidle" });

    await expect(page.getByText("No income or expense transactions found.")).toBeVisible();
    await expect(page.locator("tbody tr")).toHaveCount(0);
  });

  test("shows error state when API fails", async ({ page }) => {
    // Mock API to return error before navigation
    await page.route("**/api/balances**", (route) =>
      route.fulfill({
        status: 500,
        contentType: "text/plain",
        body: "Internal Server Error",
      }),
    );

    await page.goto("/income-statement", { waitUntil: "networkidle" });

    // Verify error alert is displayed
    const errorAlert = page.getByRole("alert");
    await expect(errorAlert).toBeVisible();
    await expect(errorAlert).toContainText("Error:");
  });
});
