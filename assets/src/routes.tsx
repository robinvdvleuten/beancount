import { Navigate } from "@solidjs/router";
import BalanceSheet from "./routes/balance-sheet";
import Editor from "./routes/editor";
import IncomeStatement from "./routes/income-statement";
import Query from "./routes/query";
import TrialBalance from "./routes/trial-balance";

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
    path: "/trial-balance",
    component: TrialBalance,
    info: {
      title: "Trial Balance",
    },
  },
  {
    path: "/query",
    component: Query,
    info: {
      title: "Query",
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
