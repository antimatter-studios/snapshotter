/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    // index.html links its favicon out of the project's assets/ directory, which
    // is above this root. Without widening the allowlist the dev server refuses
    // to serve it; the production build resolves it at bundle time regardless.
    fs: { allow: [".."] },
  },
  plugins: [react(), wails("./bindings")],
  // Everything under test here either touches the document or is imported by
  // something that does, so the DOM is on for the whole suite rather than
  // declared per file.
  test: {
    environment: "jsdom",
    // jsdom withholds localStorage from a document with an opaque origin, and
    // the theme cache reads it on the first paint. A real origin turns it on.
    environmentOptions: { jsdom: { url: "http://localhost/" } },
    setupFiles: ["./src/test-setup.ts"],
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
      coverage: {
        // The window's own code, and nothing else. Left to itself the report is
        // dominated by the generated bindings — thousands of lines of model
        // constructors nobody writes and nobody should be asked to test — which
        // pulled the headline figure to 40% while the code people actually edit
        // sat at 75%. A number that misleads in that direction is worse than
        // none, because it makes real progress invisible.
        include: ["src/**/*.ts", "src/**/*.tsx"],
        exclude: [
          "src/**/*.test.ts",
          "src/**/*.test.tsx",
          "src/test-setup.ts",
          // Catalogues are data, and their contents are checked by i18n.test.ts
          // rather than by being executed.
          "src/locales/**",
          "src/i18n/**",
          // The mount point. Rendering the whole application to execute four
          // lines would be testing the harness.
          "src/main.tsx",
        ],
      },
  },
});
