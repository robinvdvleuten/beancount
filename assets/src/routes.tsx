import { Navigate } from "@solidjs/router";
import Editor from "./routes/editor";
import IncomeStatement from "./routes/income-statement";

const routes = [
  {
    path: "/",
    component: () => <Navigate href="/income-statement" />,
  },
  {
    path: "/income-statement",
    component: IncomeStatement,
  },
  {
    path: "/editor",
    component: Editor,
  },
];

export default routes;
