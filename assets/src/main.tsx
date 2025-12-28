import { render } from "solid-js/web";
import jsonFromScript from "json-from-script";
import Application from "./components/application";
import "./style.css";

type InitialData = {
  meta: { version: string; commitSHA: string; readOnly: boolean };
};

const { meta } = jsonFromScript<InitialData>();
const elem = document.getElementById("root")!;

render(() => <Application meta={meta} />, elem);
