import { createRoot } from "react-dom/client";
import { App } from "./App";
import { FeedbackProvider } from "./feedback";
import { applyTheme, readTheme } from "./theme";
import "./styles.css";

applyTheme(readTheme());

createRoot(document.getElementById("root")!).render(
  <FeedbackProvider>
    <App />
  </FeedbackProvider>,
);
