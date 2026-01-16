// Transition helper using requestAnimationFrame for proper timing
// Inspired by: https://github.com/mmccall10/el-transition

function nextFrame(): Promise<void> {
  return new Promise((resolve) => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => resolve());
    });
  });
}

function afterTransition(element: HTMLElement): Promise<unknown[]> {
  return Promise.all(
    element.getAnimations().map((animation) => animation.finished),
  );
}

export async function enter(
  element: HTMLElement | null | undefined,
): Promise<void> {
  if (!element) return;

  element.classList.remove("hidden");
  element.classList.add("enter", "enter-start");
  await nextFrame();
  element.classList.remove("enter-start");
  element.classList.add("enter-end");
  await afterTransition(element);
  element.classList.remove("enter", "enter-end");
}

export async function leave(
  element: HTMLElement | null | undefined,
): Promise<void> {
  if (!element) return;

  element.classList.add("leave", "leave-start");
  await nextFrame();
  element.classList.remove("leave-start");
  element.classList.add("leave-end");
  await afterTransition(element);
  element.classList.remove("leave", "leave-end");
  element.classList.add("hidden");
}
