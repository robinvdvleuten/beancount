import { test, expect } from "@playwright/test";

/**
 * Editor page tests.
 *
 * Verifies the core functionality of the Beancount web editor:
 * - Page loading and rendering
 * - Content editing and saving
 * - Autocomplete functionality
 * - Syntax highlighting
 */

test.describe("Editor", () => {
  test("renders page with file content", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));

    await page.goto("/editor", { waitUntil: "networkidle" });

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
    await page.goto("/editor", { waitUntil: "networkidle" });

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();

    // Get original content
    const editorContent = page.locator(".cm-content");
    const originalText = await editorContent.textContent();

    // Click into the editor and add a comment
    await editorContent.click();
    await page.keyboard.type("\n; Test comment added by Playwright");

    // Save the file
    await page.getByRole("button", { name: "Save" }).click();
    await page.waitForLoadState("networkidle");

    // Verify no error state appeared
    const errorIndicator = page.locator('[role="alert"]');
    await expect(errorIndicator).toHaveCount(0);

    // Restore original content by selecting all and replacing
    await editorContent.click();
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type(originalText ?? "");

    // Save the restored content
    await page.getByRole("button", { name: "Save" }).click();
    await page.waitForLoadState("networkidle");
  });

  test("shows context-aware autocomplete", async ({ page }) => {
    await page.goto("/editor", { waitUntil: "networkidle" });

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();

    // Click into the editor and move to end of file
    const editorContent = page.locator(".cm-content");
    await editorContent.click();
    await page.keyboard.press("ControlOrMeta+End");

    // Type a transaction that triggers autocomplete
    await page.keyboard.type('\n\n2024-01-01 * "Test transaction"\n  Assets:U');

    // Verify autocomplete tooltip appears
    const autocompleteTooltip = page.locator(".cm-tooltip-autocomplete");
    await expect(autocompleteTooltip).toBeVisible({ timeout: 2000 });

    await page.keyboard.press("Escape");
  });

  test("renders syntax highlighting", async ({ page }) => {
    await page.goto("/editor", { waitUntil: "networkidle" });

    // Wait for editor to be visible
    const editor = page.locator(".cm-editor");
    await expect(editor).toBeVisible();

    // Verify CodeMirror renders multiple lines (example.beancount has hundreds)
    const lines = page.locator(".cm-line");
    const lineCount = await lines.count();
    expect(lineCount).toBeGreaterThan(50);
  });
});
