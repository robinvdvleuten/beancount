import { render } from "solid-js/web";
import { Router } from "@solidjs/router";
import Root from "./components/root";
import "./style.css";
import routes from "./routes";

const elem = document.getElementById("root")!;

render(
  () => (
    <Root>
      <Router>{routes}</Router>
    </Root>
  ),
  elem,
);
