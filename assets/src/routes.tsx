import { Navigate } from "@solidjs/router";
import BalanceSheet from "./routes/balance-sheet";
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
    info: {
      title: "Income Statement",
    },
  },
  {
    path: "/balance-sheet",
    component: BalanceSheet,
    info: {
      title: "Balance Sheet",
    },
  },
  {
    path: "/editor",
    component: Editor,
    info: {
      title: "Editor",
    },
  },
];

export default routes;
