// Testing Library's matchers and its automatic cleanup between tests. Without the
// cleanup, a component rendered by one test is still in the document for the
// next, and a query that should find one element finds two.
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach } from "vitest";

// Registered by hand because Testing Library only installs it automatically when
// vitest is running with globals, and this suite is not. Without it a component
// rendered by one test is still in the document for the next, so a query that
// should find one option finds five and fails on the ambiguity rather than on
// anything real.
afterEach(cleanup);

// The webview this application runs in has localStorage; the jsdom build used
// for tests does not provide one. The theme cache reads it before the first
// paint, so rather than mock it in every test that touches a theme, the suite
// supplies the same in-memory implementation the browser would give us.
//
// Storage.prototype is defined here too, because tests spy on it to imitate a
// locked-down webview where reads and writes throw.

class MemoryStorage implements Storage {
  private items = new Map<string, string>();

  get length(): number {
    return this.items.size;
  }

  clear(): void {
    this.items.clear();
  }

  getItem(key: string): string | null {
    return this.items.has(key) ? this.items.get(key)! : null;
  }

  key(index: number): string | null {
    return [...this.items.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.items.delete(key);
  }

  setItem(key: string, value: string): void {
    this.items.set(key, String(value));
  }
}

if (typeof globalThis.localStorage === "undefined") {
  const storage = new MemoryStorage();
  Object.defineProperty(globalThis, "Storage", { value: MemoryStorage, configurable: true });
  Object.defineProperty(globalThis, "localStorage", { value: storage, configurable: true });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", { value: storage, configurable: true });
  }
}

// Intervals started at import time are stopped when the file that started them
// is done with.
//
// The Wails runtime's drag module polls for its environment on import: up to a
// hundred ticks of a setInterval whose callback dereferences `window`. Nothing
// in the test suite asks for it — importing the bindings is enough — and the
// timer outlives the jsdom environment it was created in. When vitest tears that
// down between files the next tick throws "ReferenceError: window is not
// defined", which vitest reports as an unhandled error and exits non-zero on,
// with every test passing.
//
// It is a race against the length of the run rather than against anything in a
// test, so it stayed invisible until the suite grew enough to still be going a
// hundred ticks later. Clearing what a file started is the narrow fix: it does
// not silence unhandled errors, and a timer a test genuinely needs is cleared
// only after that test's file has finished.
const startedIntervals = new Set<number>();
const realSetInterval = globalThis.setInterval;
globalThis.setInterval = ((...args: Parameters<typeof realSetInterval>) => {
  const id = realSetInterval(...args);
  startedIntervals.add(id as unknown as number);
  return id;
}) as typeof realSetInterval;

afterAll(() => {
  for (const id of startedIntervals) clearInterval(id);
  startedIntervals.clear();
});
