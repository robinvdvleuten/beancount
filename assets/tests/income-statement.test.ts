import { test, expect } from "@playwright/test";

/**
 * Income Statement page tests.
 *
 * Verifies the core functionality of the Income Statement page:
 * - Page loading and rendering
 * - API data fetching and display
 * - Table structure and content
 */

test.describe("Income Statement", () => {
  test("renders page with header", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await page.goto("/income-statement", { waitUntil: "networkidle" });

    // Verify page header is visible
    await expect(
      page.getByRole("heading", { name: "Income Statement" }),
    ).toBeVisible();

    // Verify no JavaScript errors occurred
    expect(errors).toEqual([]);
  });

  test("displays table with account data", async ({ page }) => {
    await page.goto("/income-statement", { waitUntil: "networkidle" });

    // Wait for the table to be visible
    await expect(page.getByRole("table")).toBeVisible();

    // Verify table has Account column header
    await expect(
      page.getByRole("columnheader", { name: "Account" }),
    ).toBeVisible();

    // Verify table has data rows
    const tableRows = page.locator("tbody tr");
    const rowCount = await tableRows.count();
    expect(rowCount).toBeGreaterThan(0);
  });

  test("displays Income and Expenses sections", async ({ page }) => {
    await page.goto("/income-statement", { waitUntil: "networkidle" });

    // Wait for the table to be visible
    await expect(page.getByRole("table")).toBeVisible();

    // Verify Income and Expenses section headers exist
    await expect(
      page.getByRole("cell", { name: "Income", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "Expenses", exact: true }),
    ).toBeVisible();
  });

  test("displays currency columns with amounts", async ({ page }) => {
    await page.goto("/income-statement", { waitUntil: "networkidle" });

    // Wait for the table to be visible
    await expect(page.getByRole("table")).toBeVisible();

    // Verify USD currency column exists
    await expect(
      page.getByRole("columnheader", { name: "USD", exact: true }),
    ).toBeVisible();

    // Verify amounts are displayed (monospace cells contain formatted numbers)
    const amountCells = page.locator("td.font-mono");
    const amountCount = await amountCells.count();
    expect(amountCount).toBeGreaterThan(0);
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
    await page.goto("/income-statement", { waitUntil: "networkidle" });

    // Wait for the table to be visible
    await expect(page.getByRole("table")).toBeVisible();

    // Verify parent accounts (Income, Expenses) are displayed as section headers
    await expect(
      page.getByRole("cell", { name: "Income", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "Expenses", exact: true }),
    ).toBeVisible();

    // Verify child accounts exist (rows without bg-base-200 are leaf accounts)
    const headerRows = page.locator("tbody tr.bg-base-200");
    const allRows = page.locator("tbody tr");
    const headerCount = await headerRows.count();
    const totalCount = await allRows.count();

    // There should be more total rows than header rows (i.e., child accounts exist)
    expect(totalCount).toBeGreaterThan(headerCount);
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
