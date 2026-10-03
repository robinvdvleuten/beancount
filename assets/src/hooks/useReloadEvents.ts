import { onCleanup, onMount } from "solid-js";
import { meta } from "virtual:globals";

interface ReloadEventHandlers {
  /** Called when the server reloads the ledger */
  onReload: () => void;
  /** Called when the connection to the server opens again */
  onOpen?: () => void;
  /** Called when the connection to the server is lost */
  onError?: () => void;
}

/**
 * Hook for the server's live-reload events, while the component lives.
 * Connects only when the server watches the ledger's files.
 */
export function useReloadEvents(handlers: ReloadEventHandlers) {
  onMount(() => {
    if (!meta.watching) {
      return;
    }

    const eventSource = new EventSource("/api/events");

    eventSource.onmessage = (event: MessageEvent<string>) => {
      // The server greets each connection with "connected"; only "reload" means a change
      if (event.data === "reload") {
        handlers.onReload();
      }
    };
    eventSource.onopen = () => handlers.onOpen?.();
    eventSource.onerror = () => handlers.onError?.();

    onCleanup(() => {
      eventSource.close();
    });
  });
}
