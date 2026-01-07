import type { ParentComponent } from "solid-js";
import { meta } from "virtual:globals";

const Root: ParentComponent = (props) => {
  return (
    <div class="flex h-screen flex-col">
      {props.children}

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
