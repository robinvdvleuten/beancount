import { test, expect, type Page } from "@playwright/test";

// Expected amounts come from bean-query on testdata/example.beancount.

async function gotoReport(page: Page, path: string) {
  const balancesLoaded = page.waitForResponse(
    (response) => response.url().includes("/api/balances") && response.ok(),
  );
  await page.goto(path);
  await balancesLoaded;
}

function accountRow(page: Page, tableName: string, accountName: string) {
  return page
    .getByRole("table", { name: tableName })
    .locator("tbody tr")
    .filter({ has: page.getByRole("cell", { name: accountName, exact: true }) })
    .first();
}

test.describe("Report period", () => {
  test("income statement sums the chosen period", async ({ page }) => {
    await gotoReport(page, "/income-statement?from=2022-01-01&to=2022-12-31");

    await expect(page.getByLabel("From")).toHaveValue("2022-01-01");
    await expect(page.getByLabel("To")).toHaveValue("2022-12-31");
    // select sum(position) where account = 'Expenses:Food:Restaurant' and year = 2022
    await expect(accountRow(page, "Expenses", "Restaurant")).toContainText("4,987.25");
  });

  test("a one-day period holds only that day's postings", async ({ page }) => {
    await gotoReport(page, "/income-statement?from=2022-01-05&to=2022-01-05");

    // select sum(position) where account ~ '^Expenses' and date = 2022-01-05
    await expect(accountRow(page, "Expenses", "Expenses")).toContainText("2,565.00");
    await expect(accountRow(page, "Expenses", "Restaurant")).toContainText("25.25");
  });

  test("balance sheet shows balances as of the chosen date", async ({ page }) => {
    await gotoReport(page, "/balance-sheet?asOf=2022-06-30");

    await expect(page.getByLabel("As of")).toHaveValue("2022-06-30");
    // select sum(position) where account = 'Assets:US:BofA:Checking' and date <= 2022-06-30
    await expect(accountRow(page, "Assets", "Checking")).toContainText("3,346.48");
  });

  test("the controls update the URL and back restores the previous period", async ({ page }) => {
    await gotoReport(page, "/income-statement");

    await page.getByLabel("From").fill("2022-01-05");
    await expect(page).toHaveURL(/from=2022-01-05/);
    await page.getByLabel("To").fill("2022-01-05");
    await expect(page).toHaveURL(/from=2022-01-05&to=2022-01-05/);
    await expect(accountRow(page, "Expenses", "Expenses")).toContainText("2,565.00");

    await page.goBack();
    await expect(page).toHaveURL(/from=2022-01-05$/);
    await expect(page.getByLabel("To")).toHaveValue("");

    await page.getByRole("button", { name: "All time" }).click();
    await expect(page).toHaveURL(/\/income-statement$/);
  });

  test("an invalid range shows the API's error", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await page.goto("/income-statement?from=2022-12-31&to=2022-01-01");

    await expect(page.getByRole("alert")).toContainText("is after endDate");
    await expect(page.getByLabel("From")).toBeVisible();
    expect(errors).toEqual([]);
  });

  test("a start without an end runs through the last entry", async ({ page }) => {
    await gotoReport(page, "/income-statement?from=2022-01-05");

    await expect(page.getByRole("alert")).toHaveCount(0);
    // select sum(position) where account = 'Expenses:Food:Restaurant' and date >= 2022-01-05
    await expect(accountRow(page, "Expenses", "Restaurant")).toContainText("7,654.78");
  });
});
