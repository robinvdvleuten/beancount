import { test, expect, type Page } from "@playwright/test";

// Expected amounts come from bean-query on testdata/example.beancount:
//   select cost(sum(position)), value(sum(position)) where account ~ '^Assets'

async function gotoReport(page: Page, path: string) {
  const balancesLoaded = page.waitForResponse(
    (response) => response.url().includes("/api/balances") && response.ok(),
  );
  await page.goto(path);
  await balancesLoaded;
}

function assetsRow(page: Page) {
  return page
    .getByRole("table", { name: "Assets" })
    .locator("tbody tr")
    .filter({ has: page.getByRole("cell", { name: "Assets", exact: true }) })
    .first();
}

test.describe("Valuation", () => {
  test("reports value at cost by default", async ({ page }) => {
    await gotoReport(page, "/balance-sheet");

    await expect(page.getByLabel("Valuation")).toHaveValue("");
    await expect(page.getByLabel("Valuation").locator("option:checked")).toHaveText("At cost");
    await expect(assetsRow(page)).toContainText("101,981.88");
    await expect(page).toHaveURL(/\/balance-sheet$/);
  });

  test("offers units, cost, market value and each operating currency", async ({ page }) => {
    await gotoReport(page, "/balance-sheet");

    await expect(page.getByLabel("Valuation").locator("option")).toHaveText([
      "Units",
      "At cost",
      "At market value",
      "Converted to USD",
    ]);
  });

  test("switching records the choice in the URL", async ({ page }) => {
    await gotoReport(page, "/balance-sheet");

    await page.getByLabel("Valuation").selectOption({ label: "At market value" });
    await expect(page).toHaveURL(/valuation=market/);
    await expect(assetsRow(page)).toContainText("113,806.49");

    await page.getByLabel("Valuation").selectOption({ label: "Units" });
    await expect(page).toHaveURL(/valuation=units/);
    await expect(assetsRow(page)).toContainText("RGAGX");

    await page.getByLabel("Valuation").selectOption({ label: "At cost" });
    await expect(page).toHaveURL(/\/balance-sheet$/);
    await expect(assetsRow(page)).toContainText("101,981.88");
  });

  test("the sidebar links carry the choice to the other reports", async ({ page }) => {
    await gotoReport(page, "/balance-sheet?valuation=USD");
    await expect(page.getByLabel("Valuation").locator("option:checked")).toHaveText(
      "Converted to USD",
    );

    await page.getByRole("link", { name: "Trial Balance" }).click();
    await expect(page).toHaveURL(/\/trial-balance\?valuation=USD$/);
    await expect(page.getByLabel("Valuation").locator("option:checked")).toHaveText(
      "Converted to USD",
    );

    await page.getByRole("link", { name: "Income Statement" }).click();
    await expect(page).toHaveURL(/\/income-statement\?valuation=USD$/);
    await expect(page.getByLabel("Valuation").locator("option:checked")).toHaveText(
      "Converted to USD",
    );
  });
});
