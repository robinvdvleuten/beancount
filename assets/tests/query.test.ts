import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { test, expect, type Page } from "@playwright/test";

// The binary and ledger the Playwright web server runs (see playwright.config.ts)
const binary = fileURLToPath(new URL("../../beancount", import.meta.url));
const ledger = fileURLToPath(new URL("../../testdata/example.beancount", import.meta.url));

// cli returns what `beancount query` prints for statement.
const cli = (statement: string, ...flags: string[]): string =>
  execFileSync(binary, ["query", ...flags, ledger, statement], { encoding: "utf8" });

async function waitForQuery(page: Page, action: () => Promise<unknown>) {
  const queried = page.waitForResponse(
    (response) => response.url().endsWith("/api/query") && response.ok(),
  );
  await action();
  await queried;
}

const output = (page: Page) => page.getByLabel("Query output");

test.describe("Query", () => {
  test("prints what beancount query prints", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    const statement = "select account, sum(position) group by account";

    await page.goto("/query");
    await expect(page.getByRole("heading", { name: "Query" })).toBeVisible();
    await page.getByLabel("Query", { exact: true }).fill(statement);
    await waitForQuery(page, () => page.getByRole("button", { name: "Run" }).click());

    expect(await output(page).textContent()).toBe(cli(statement));
    expect(errors).toEqual([]);
  });

  test("prints csv like beancount query -f csv", async ({ page }) => {
    const statement = "select date, narration where account ~ 'Restaurant' limit 5";

    await waitForQuery(page, () =>
      page.goto(`/query?format=csv&q=${encodeURIComponent(statement)}`),
    );

    await expect(page.getByLabel("Format")).toHaveValue("csv");
    expect(await output(page).textContent()).toBe(cli(statement, "-f", "csv"));
  });

  test("runs with Cmd/Ctrl+Enter and keeps the statement in the URL", async ({ page }) => {
    const input = page.getByLabel("Query", { exact: true });

    await page.goto("/query");
    await input.fill("select count(date)");
    await waitForQuery(page, () => input.press("ControlOrMeta+Enter"));

    await expect(page).toHaveURL(/\?q=select(\+|%20)count/);
    expect(await output(page).textContent()).toBe(cli("select count(date)"));

    await waitForQuery(page, () => page.reload());
    await expect(input).toHaveValue("select count(date)");
    expect(await output(page).textContent()).toBe(cli("select count(date)"));
  });

  test("says so when a query has no rows", async ({ page }) => {
    const statement = "select account where account = 'Nope'";

    await waitForQuery(page, () => page.goto(`/query?q=${encodeURIComponent(statement)}`));

    expect(cli(statement)).toBe("");
    await expect(page.getByText("No rows.")).toBeVisible();
    await expect(output(page)).toHaveCount(0);
  });

  test("shows the error beancount query prints for a statement that does not compile", async ({
    page,
  }) => {
    await waitForQuery(page, () => page.goto("/query?q=select%20nosuchcolumn"));

    // beancount query prints it on stderr and exits 1, like bean-query
    const failed = spawnSync(binary, ["query", ledger, "select nosuchcolumn"], {
      encoding: "utf8",
    });
    expect(failed.status).toBe(1);
    expect(failed.stdout).toBe("");
    expect(await output(page).textContent()).toBe(failed.stderr);
    await expect(output(page)).toContainText(
      'error: column "nosuchcolumn" not found in table "postings"',
    );
  });

  test("prints the directives a print statement selects", async ({ page }) => {
    const statement = "print from date = 2022-01-05";

    await waitForQuery(page, () => page.goto(`/query?q=${encodeURIComponent(statement)}`));

    await expect(output(page)).toContainText(
      '2022-01-05 * "Chichipotle" "Eating out with Natasha"',
    );
    expect(await output(page).textContent()).toBe(cli(statement));
  });

  test("shows the server's error when the request fails", async ({ page }) => {
    await page.route("**/api/query", (route) =>
      route.fulfill({ status: 409, contentType: "text/plain", body: "the ledger failed to load" }),
    );

    await page.goto("/query?q=select%201");

    await expect(page.getByRole("alert")).toContainText("the ledger failed to load");
  });
});
