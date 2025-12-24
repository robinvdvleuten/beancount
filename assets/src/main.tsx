import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import jsonFromScript from "json-from-script";
import Application from "./components/application";
import "./style.css";

type InitialData = {
  meta: { version: string; commitSHA: string; readOnly: boolean };
};

const { meta } = jsonFromScript<InitialData>();
const elem = document.getElementById("root")!;

const app = (
  <StrictMode>
    <Application meta={meta} />
  </StrictMode>
);

createRoot(elem).render(app);
