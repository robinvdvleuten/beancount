import { render } from "solid-js/web";
import { Router } from "@solidjs/router";
import Root from "./components/root";
import routes from "./routes";
import "./style.css";

const elem = document.getElementById("root")!;

render(() => <Router root={Root}>{routes}</Router>, elem);
