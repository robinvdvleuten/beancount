import {
  type Component,
  Match,
  Show,
  Switch,
  createEffect,
  createResource,
  createSignal,
} from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { useFileChange } from "../hooks/useFileChange";
import { type QueryFormat, runQuery } from "../lib/query";
import { FinancialReport } from "../components/financial-report";

const Query: Component = () => {
  // The statement and format live in the URL (?q=&format=), so reloading or
  // sharing runs the same query
  const [searchParams, setSearchParams] = useSearchParams<{ q: string; format: string }>();
  const format = (): QueryFormat => (searchParams.format === "csv" ? "csv" : "text");
  const [text, setText] = createSignal("");

  // Keep the input in step with the URL, e.g. after the back button
  createEffect(() => setText(searchParams.q ?? ""));

  const [result, { refetch }] = createResource(
    () => (searchParams.q ? { query: searchParams.q, format: format() } : false),
    (source) => runQuery(source.query, source.format),
  );

  const run = () => {
    const query = text().trim();
    if (!query) return;
    if (query === searchParams.q) {
      void refetch();
    } else {
      setSearchParams({ q: query });
    }
  };

  // File change detection via SSE - click to reload
  const fileChange = useFileChange({
    getLastFingerprint: () => undefined, // No fingerprint tracking needed
    onReload: () => {
      void refetch();
    },
  });

  return (
    <>
      <FinancialReport.Root>
        <form
          class="mb-4 flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            run();
          }}
        >
          <textarea
            aria-label="Query"
            class="textarea w-full font-mono text-sm"
            rows={4}
            placeholder="select account, sum(position) group by account"
            value={text()}
            onInput={(event) => setText(event.currentTarget.value)}
            onKeyDown={(event) => {
              if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
                event.preventDefault();
                run();
              }
            }}
          />
          <div class="flex items-center gap-2">
            <button type="submit" class="btn btn-primary btn-sm">
              Run
            </button>
            <select
              aria-label="Format"
              class="select w-auto select-sm"
              value={format()}
              onChange={(event) =>
                setSearchParams({ format: event.currentTarget.value === "csv" ? "csv" : undefined })
              }
            >
              <option value="text">Text</option>
              <option value="csv">CSV</option>
            </select>
            <span class="text-xs text-base-content/50">⌘/Ctrl + Enter to run</span>
          </div>
        </form>

        <Switch>
          <Match when={result.loading}>
            <FinancialReport.Loading />
          </Match>

          <Match when={result.error as Error | undefined}>
            {(error) => <FinancialReport.Error error={error()} />}
          </Match>

          <Match when={result() !== undefined}>
            <pre
              aria-label="Query output"
              class="overflow-auto rounded-box bg-base-200 p-4 font-mono text-sm"
            >
              {result()}
            </pre>
          </Match>
        </Switch>
      </FinancialReport.Root>

      {/* External file change toast - click to reload */}
      <Show when={fileChange.pendingReload()}>
        <div class="toast toast-end">
          <div
            ref={fileChange.setToastRef}
            class="alert hidden cursor-pointer alert-info"
            onClick={fileChange.handleReloadClick}
          >
            <span>File changed — click to reload</span>
          </div>
        </div>
      </Show>
    </>
  );
};

export default Query;
