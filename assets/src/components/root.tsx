import { type ParentComponent, createEffect, createMemo } from "solid-js";
import { A, useCurrentMatches } from "@solidjs/router";
import DocumentCurrencyDollarIcon from "heroicons/24/solid/document-currency-dollar.svg?component-solid";
import { meta } from "virtual:globals";

interface MenuItemProps {
  href: string;
}

const MenuItem: ParentComponent<MenuItemProps> = (props) => {
  return (
    <li>
      <A href={props.href} class="rounded-none" end>
        {props.children}
      </A>
    </li>
  );
};

const Root: ParentComponent = (props) => {
  const matches = useCurrentMatches();
  const title = createMemo<string | undefined>(() =>
    matches()
      .map((m) => m.route.info?.title as string)
      .find(Boolean),
  );

  createEffect(() => {
    document.title = title() ? `${title()} - ${meta.title}` : meta.title;
  });

  return (
    <div class="flex h-screen flex-col">
      <header class="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div class="flex items-center gap-3">
          <div class="text-primary">
            <DocumentCurrencyDollarIcon class="size-8" />
          </div>
          <h1 class="text-xl font-semibold">
            <span class="text-base-content/60">{meta.title}</span>
            <span class="px-2 text-base-content/40" aria-hidden="true">
              ›
            </span>
            {title()}
          </h1>
        </div>
      </header>

      <div class="flex flex-1 overflow-hidden">
        <aside class="w-56 border-r border-base-300 bg-base-200">
          <ul class="menu w-full px-0 [--menu-active-bg:var(--color-base-300)] [--menu-active-fg:var(--color-base-content)]">
            <MenuItem href="/income-statement">Income Statement</MenuItem>
            <MenuItem href="/balance-sheet">Balance Sheet</MenuItem>
            <MenuItem href="/trial-balance">Trial Balance</MenuItem>
            <MenuItem href="/editor">Editor</MenuItem>
          </ul>
        </aside>
        <main class="flex flex-1 flex-col overflow-hidden">{props.children}</main>
      </div>

      <footer class="flex items-center justify-between border-t border-base-300 px-6 py-2">
        <div class="text-xs text-base-content/70">
          {meta.version}
          {meta.commitSHA && ` (${meta.commitSHA})`}
          {meta.readOnly && " read-only mode"}
        </div>
      </footer>
    </div>
  );
};

export default Root;
