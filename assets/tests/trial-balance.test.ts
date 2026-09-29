import { test, expect, type Page } from "@playwright/test";

async function navigateTo(page: Page, path: string) {
  const balancesLoaded = page.waitForResponse(
    (response) => response.url().includes("/api/balances") && response.ok(),
  );

  await page.goto(path);
  await balancesLoaded;
}

function rootRow(page: Page, tableName: string) {
  return page
    .getByRole("table", { name: tableName })
    .locator("tbody tr")
    .filter({ has: page.getByRole("cell", { name: tableName, exact: true }) })
    .first();
}

test.describe("Trial Balance", () => {
  test("renders every account type in the ledger's order", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await navigateTo(page, "/trial-balance");

    await expect(page.getByRole("heading", { name: "Trial Balance" })).toBeVisible();
    await expect(page).toHaveTitle("Trial Balance - Example Beancount file");
    await expect
      .poll(() =>
        page
          .getByRole("table")
          .evaluateAll((tables) => tables.map((table) => table.getAttribute("aria-label"))),
      )
      .toEqual(["Assets", "Liabilities", "Equity", "Income", "Expenses"]);

    expect(errors).toEqual([]);
  });

  test("marks its sidebar link as the current page", async ({ page }) => {
    await navigateTo(page, "/trial-balance");

    await expect(page.getByRole("link", { name: "Trial Balance" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    // The current page's link keeps the text color of the others. The tests
    // compile without the DOM lib, so the page evaluates these as strings.
    const linkColor = (current: boolean) =>
      page.evaluate(
        `getComputedStyle(document.querySelector('aside a${current ? "[aria-current=page]" : ":not([aria-current])"}')).color`,
      );
    expect(await linkColor(true)).toEqual(await linkColor(false));
  });

  test("totals match the balance sheet and income statement", async ({ page }) => {
    await navigateTo(page, "/trial-balance");
    const assets = await rootRow(page, "Assets").textContent();
    const expenses = await rootRow(page, "Expenses").textContent();

    await navigateTo(page, "/balance-sheet");
    await expect(rootRow(page, "Assets")).toHaveText(assets ?? "");

    await navigateTo(page, "/income-statement");
    await expect(rootRow(page, "Expenses")).toHaveText(expenses ?? "");
  });

  test("names each table after the ledger's account roots", async ({ page }) => {
    await page.route("**/api/balances**", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          roots: [
            {
              name: "Activa",
              depth: 0,
              balance: { EUR: "10" },
              children: [
                { name: "Activa:Bank", account: "Activa:Bank", depth: 1, balance: { EUR: "10" } },
              ],
            },
          ],
          currencies: ["EUR"],
          operatingCurrencies: ["EUR"],
        }),
      }),
    );

    await page.goto("/trial-balance", { waitUntil: "networkidle" });

    await expect(rootRow(page, "Activa")).toContainText("10.00");
    await expect(page.getByRole("cell", { name: "Bank", exact: true })).toBeVisible();
  });

  test("shows empty state when API returns no rows", async ({ page }) => {
    await page.route("**/api/balances**", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ roots: [], currencies: [], operatingCurrencies: [] }),
      }),
    );

    await page.goto("/trial-balance", { waitUntil: "networkidle" });

    await expect(page.getByText("No accounts found.")).toBeVisible();
  });

  test("shows error state when API fails", async ({ page }) => {
    await page.route("**/api/balances**", (route) =>
      route.fulfill({ status: 500, contentType: "text/plain", body: "Internal Server Error" }),
    );

    await page.goto("/trial-balance", { waitUntil: "networkidle" });

    await expect(page.getByRole("alert")).toContainText("Error:");
  });
});
