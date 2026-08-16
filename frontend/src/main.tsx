import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { applyTheme, storedTheme } from "./theme";
import { TranslationProvider } from "./i18n";
import "./styles.css";

// Applied before the first paint. Doing it inside a component would show the
// default palette for a frame and then swap, which on launch reads as a flicker.
applyTheme(storedTheme());

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <TranslationProvider>
        <App />
      </TranslationProvider>
  </React.StrictMode>,
);
