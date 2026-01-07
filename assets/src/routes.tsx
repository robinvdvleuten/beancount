import { Navigate } from "@solidjs/router";
import Editor from "./routes/editor";

const routes = [
  {
    path: "/",
    component: () => <Navigate href="/editor" />,
  },
  {
    path: "/editor",
    component: Editor,
  },
];

export default routes;
