import { createSignal, onCleanup, onMount } from "solid-js";
import { enter, leave } from "../lib/transition";
import { meta } from "virtual:globals";

interface UseFileChangeOptions {
  /** Returns the last known fingerprint (from save or load) */
  getLastFingerprint: () => string | undefined;
  /** Called when user clicks to reload */
  onReload: () => void;
}

/**
 * Hook for detecting external file changes via SSE.
 * Shows a "click to reload" toast when files change externally.
 * Skips notification if fingerprint matches (our own save triggered the event).
 */
export function useFileChange(options: UseFileChangeOptions) {
  const [pendingReload, setPendingReload] = createSignal(false);
  const [connectionLost, setConnectionLost] = createSignal(false);
  let toastRef: HTMLDivElement | undefined;
  let lastSavedFingerprint: string | undefined;

  const showToast = async () => {
    setPendingReload(true);
    await Promise.resolve(); // Wait for DOM update
    await enter(toastRef);
  };

  const hideToast = async () => {
    await leave(toastRef);
    setPendingReload(false);
  };

  const handleReloadClick = () => {
    void hideToast();
    lastSavedFingerprint = undefined;
    options.onReload();
  };

  /** Mark that we just saved - next SSE event with matching fingerprint will be ignored */
  const markSaved = (fingerprint: string) => {
    lastSavedFingerprint = fingerprint;
  };

  onMount(() => {
    // Only connect if watch mode is enabled on the server
    if (!meta.watching) {
      return;
    }

    const eventSource = new EventSource("/api/events");

    eventSource.onmessage = () => {
      // Skip if this is our own save (fingerprint will match)
      const currentFingerprint = options.getLastFingerprint();
      if (lastSavedFingerprint && lastSavedFingerprint === currentFingerprint) {
        lastSavedFingerprint = undefined; // Clear after comparison
        return;
      }

      // External change - show toast
      if (!pendingReload()) {
        void showToast();
      }
    };

    eventSource.onopen = () => {
      setConnectionLost(false);
    };

    eventSource.onerror = () => {
      console.warn("SSE connection lost, will reconnect...");
      setConnectionLost(true);
    };

    onCleanup(() => {
      eventSource.close();
    });
  });

  return {
    pendingReload,
    connectionLost,
    setToastRef: (el: HTMLDivElement) => {
      toastRef = el;
    },
    handleReloadClick,
    markSaved,
  };
}
