import { type ParentComponent, createMemo } from "solid-js";
import { A, useCurrentMatches } from "@solidjs/router";
import DocumentCurrencyDollarIcon from "heroicons/24/solid/document-currency-dollar.svg?component-solid";
import { meta } from "virtual:globals";

interface MenuItemProps {
  href: string;
}

const MenuItem: ParentComponent<MenuItemProps> = (props) => {
  return (
    <li>
      <A href={props.href} class="rounded-none" activeClass="bg-base-300" end>
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

  return (
    <div class="flex h-screen flex-col">
      <header class="flex items-center justify-between border-b border-base-300 px-6 py-2">
        <div class="flex items-center gap-3">
          <div class="text-primary">
            <DocumentCurrencyDollarIcon class="size-8" />
          </div>
          <h1 class="text-xl font-semibold">{title()}</h1>
        </div>
      </header>

      <div class="flex flex-1 overflow-hidden">
        <aside class="w-56 border-r border-base-300 bg-base-200">
          <ul class="menu px-0 w-full">
            <MenuItem href="/income-statement">Income Statement</MenuItem>
            <MenuItem href="/editor">Editor</MenuItem>
          </ul>
        </aside>
        <main class="flex flex-1 flex-col overflow-hidden">
          {props.children}
        </main>
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
