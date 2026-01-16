import { createSignal, onCleanup } from "solid-js";
import { enter, leave } from "../lib/transition";

const TOAST_DISPLAY_MS = 3000;

/**
 * Hook for showing a temporary success toast.
 */
export function useToast() {
  const [visible, setVisible] = createSignal(false);
  let toastRef: HTMLDivElement | undefined;
  let hideTimeout: number | undefined;

  const show = async () => {
    if (hideTimeout) clearTimeout(hideTimeout);

    setVisible(true);
    await Promise.resolve();
    await enter(toastRef);

    hideTimeout = window.setTimeout(async () => {
      await leave(toastRef);
      setVisible(false);
    }, TOAST_DISPLAY_MS);
  };

  onCleanup(() => {
    if (hideTimeout) clearTimeout(hideTimeout);
  });

  return {
    visible,
    show,
    setToastRef: (el: HTMLDivElement) => {
      toastRef = el;
    },
  };
}
