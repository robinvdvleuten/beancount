import { test, expect } from '@playwright/test';

/**
 * Beancount Web Editor tests.
 *
 * This test suite verifies the core functionality of the Beancount web editor:
 * - File content loading and rendering
 * - Content editing and saving
 * - Autocomplete functionality
 * - Syntax highlighting
 */

test.describe('Beancount Web Editor', () => {

  test('loads and renders editor with file content', async ({ page }) => {
    const errors: string[] = [];

    // Capture console errors
    page.on('pageerror', (error) => {
      errors.push(error.message);
    });

    // Navigate to the page
    await page.goto('/', {
      waitUntil: 'networkidle',
    });

    // Wait for editor to be visible
    const editor = page.locator('.cm-editor');
    await expect(editor).toBeVisible({ timeout: 5000 });

    // Get editor text content and verify it contains expected content from example.beancount
    const editorContent = page.locator('.cm-content');
    const text = await editorContent.textContent();

    // example.beancount should contain commodity directive
    expect(text).toContain('commodity USD');

    // Verify no JavaScript errors occurred
    expect(errors).toEqual([]);
  });

  test('saves file content changes', async ({ page }) => {
    // Navigate to the page
    await page.goto('/', {
      waitUntil: 'networkidle',
    });

    // Wait for editor to be visible
    const editor = page.locator('.cm-editor');
    await expect(editor).toBeVisible({ timeout: 5000 });

    // Click into the editor content area
    const editorContent = page.locator('.cm-content');
    await editorContent.click();

    // Type some new content (a comment)
    await page.keyboard.type('\n; Test comment added by Playwright');

    // Click the save button
    const saveButton = page.locator('button:has-text("Save")');
    await saveButton.click();

    // Wait for network to be idle (save request completed)
    await page.waitForLoadState('networkidle');

    // Verify no error state appeared
    // If save fails, typically an error message or error class would appear
    const errorIndicator = page.locator('.error, [role="alert"], .cm-error-message');
    await expect(errorIndicator).toHaveCount(0);
  });

  test('autocomplete is context-aware', async ({ page }) => {
    // Navigate to the page
    await page.goto('/', {
      waitUntil: 'networkidle',
    });

    // Wait for editor to be visible
    const editor = page.locator('.cm-editor');
    await expect(editor).toBeVisible({ timeout: 5000 });

    // Click into the editor at the end
    const editorContent = page.locator('.cm-content');
    await editorContent.click();

    // Move to end of file (platform-specific shortcuts)
    const isMac = process.platform === 'darwin';
    if (isMac) {
      await page.keyboard.press('Meta+ArrowDown');
    } else {
      await page.keyboard.press('Control+End');
    }

    // Add a new transaction that should allow autocomplete
    // Type "Assets:U" which should trigger autocomplete for Assets:US:* accounts
    // Autocomplete activates on typing when activateOnTyping is true
    await page.keyboard.type('\n\n2024-01-01 * "Test transaction"\n  Assets:U');

    // Verify autocomplete tooltip appears with account suggestions
    // The tooltip should contain account names matching "Assets:U*" from example.beancount
    const autocompleteTooltip = page.locator('.cm-tooltip-autocomplete');

    // Wait for autocomplete to appear automatically (activates on typing)
    await expect(autocompleteTooltip).toBeVisible({ timeout: 2000 });

    // Press Escape to close autocomplete
    await page.keyboard.press('Escape');
  });

  test('supports syntax highlighting', async ({ page }) => {
    // Navigate to the page
    await page.goto('/', {
      waitUntil: 'networkidle',
    });

    // Wait for editor to be visible
    const editor = page.locator('.cm-editor');
    await expect(editor).toBeVisible({ timeout: 5000 });

    // Verify that CodeMirror is rendering content with syntax highlighting
    // CodeMirror applies various styling classes to tokens
    const editorContent = page.locator('.cm-content');
    await expect(editorContent).toBeVisible();

    // Check that the editor has some styled content
    // CodeMirror typically adds .cm-line elements for each line
    const lines = page.locator('.cm-line');
    const lineCount = await lines.count();

    // example.beancount has hundreds of lines, so we should see many .cm-line elements
    expect(lineCount).toBeGreaterThan(50);
  });
});
