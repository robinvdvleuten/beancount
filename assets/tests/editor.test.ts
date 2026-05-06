import { test, expect } from "@playwright/test";

/**
 * Editor page tests.
 *
 * Verifies the core functionality of the Beancount web editor:
 * - Page loading and rendering
 * - Content editing and saving
 * - Autocomplete functionality
 * - Syntax highlighting
 * - File selector dropdown (when includes exist)
 */

async function navigateToEditor(page: import("@playwright/test").Page) {
  await page.goto("/editor", { waitUntil: "networkidle" });
  await page.waitForURL("/editor");
}

async function getCurrentSource(page: import("@playwright/test").Page) {
  const response = await page.request.get("/api/source");
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as { source: string };
}

async function restoreSource(
  page: import("@playwright/test").Page,
  source: string,
) {
  const response = await page.request.put("/api/source", {
    data: { source },
  });
  expect(response.ok()).toBeTruthy();
}

test.describe("Editor", () => {
  test("renders page with file content", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await navigateToEditor(page);

    // Verify editor is visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();

    // Verify file content is loaded (example.beancount contains commodity directive)
    const editorContent = page.locator(".cm-content");
    await expect(editorContent).toContainText("commodity USD");

    // Verify no JavaScript errors occurred
    expect(errors).toEqual([]);
  });

  test("saves and restores file content", async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text());
      }
    });

    await navigateToEditor(page);

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();
    await expect(page.locator(".cm-content")).toContainText("commodity USD");

    // Read the real source from the API so restore doesn't depend on CodeMirror's viewport DOM.
    const { source: originalSource } = await getCurrentSource(page);

    const editorContent = page.locator(".cm-content");

    try {
      // Click into the editor and add a comment
      const testComment = "; Test comment added by Playwright";
      await editorContent.click();
      await page.keyboard.press("ControlOrMeta+End");
      await page.keyboard.type(`\n${testComment}`);

      // Save the file with the editor shortcut
      const saveResponsePromise = page.waitForResponse(
        (response) =>
          response.url().includes("/api/source") &&
          response.request().method() === "PUT",
      );
      await page.keyboard.press("ControlOrMeta+KeyS");
      const saveResponse = await saveResponsePromise;
      expect(saveResponse.ok()).toBeTruthy();

      const savedBody = (await saveResponse.json()) as { source: string };
      expect(savedBody.source).toContain(testComment);

      // Verify the save completed in the UI and persisted to disk.
      await expect(page.getByText("File saved")).toBeVisible();

      const { source: savedSource } = await getCurrentSource(page);
      expect(savedSource).toContain(testComment);
      expect(consoleErrors).toEqual([]);
    } finally {
      await restoreSource(page, originalSource);
    }
  });

  test("shows diagnostics after saving invalid content", async ({ page }) => {
    await navigateToEditor(page);

    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();
    await expect(page.locator(".cm-content")).toContainText("commodity USD");

    const { source: originalSource } = await getCurrentSource(page);
    const invalidSource = "12345";
    const editorContent = page.locator(".cm-content");

    try {
      await editorContent.click();
      await page.keyboard.press("ControlOrMeta+a");
      await page.keyboard.type(invalidSource);

      const saveResponsePromise = page.waitForResponse(
        (response) =>
          response.url().includes("/api/source") &&
          response.request().method() === "PUT",
      );
      await page.getByRole("button", { name: "Save" }).click();
      const saveResponse = await saveResponsePromise;
      expect(saveResponse.ok()).toBeTruthy();

      const savedBody = (await saveResponse.json()) as {
        source: string;
        errors: Array<{ type: string }>;
      };
      expect(savedBody.source).toBe(invalidSource);
      expect(savedBody.errors).toHaveLength(1);
      expect(savedBody.errors[0]?.type).toBe("ParseError");

      await expect(page.locator(".cm-lintRange-error")).toHaveCount(1);
    } finally {
      await restoreSource(page, originalSource);
    }
  });

  test("shows context-aware autocomplete", async ({ page }) => {
    const accountsLoaded = page.waitForResponse(
      (response) => response.url().includes("/api/accounts") && response.ok(),
    );

    await navigateToEditor(page);
    await accountsLoaded;

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();
    await expect(page.locator(".cm-content")).toContainText("commodity USD");

    // Click into the editor and move to end of file
    const editorContent = page.locator(".cm-content");
    await editorContent.click();
    await page.keyboard.press("ControlOrMeta+End");

    // Type a transaction that triggers autocomplete
    await page.keyboard.type('\n\n2024-01-01 * "Test transaction"\n  Assets:U');
    await page.keyboard.press("ControlOrMeta+Space");

    // Verify autocomplete tooltip appears
    const autocompleteTooltip = page.locator(".cm-tooltip-autocomplete");
    await expect(autocompleteTooltip).toBeVisible();
    await expect(autocompleteTooltip).toContainText("Assets:US:BofA");

    await page.keyboard.press("Escape");
  });

  test("renders syntax highlighting", async ({ page }) => {
    await navigateToEditor(page);

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();
    const editorContent = page.locator(".cm-content");
    await expect(editorContent).toContainText("commodity USD");

    // Verify syntax highlighting is applied after CodeMirror finishes rendering tokens.
    const styledSpans = editorContent.locator("span[class]");
    await expect.poll(async () => await styledSpans.count()).toBeGreaterThan(0);
  });

  test("shows static filepath when no includes", async ({ page }) => {
    await navigateToEditor(page);

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();

    // When there are no includes, should show static filepath text (not dropdown)
    // The example.beancount file has no includes
    const filepathText = page.getByLabel("Current file");
    await expect(filepathText).toBeVisible();
    await expect(filepathText).toContainText("example.beancount");

    // File selector dropdown should not exist (only shown when includes exist)
    const fileSelector = page.getByLabel("Select file");
    await expect(fileSelector).toHaveCount(0);
  });
});
