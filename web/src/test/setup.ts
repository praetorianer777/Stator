import "@testing-library/jest-dom/vitest";

/**
 * Node 26 ships an experimental built-in `localStorage` that is inert unless the
 * runtime is started with --localstorage-file, and it takes precedence over the
 * one jsdom provides. The result is that `localStorage` is undefined inside
 * tests even though the browser the code actually runs in has it.
 *
 * Rather than pin the whole toolchain to an older Node, install a minimal
 * in-memory implementation of the Storage interface. Tests that need to
 * simulate a browser refusing access still can, by spying on Storage.prototype.
 */
class MemoryStorage implements Storage {
  #entries = new Map<string, string>();

  get length(): number {
    return this.#entries.size;
  }

  clear(): void {
    this.#entries.clear();
  }

  getItem(key: string): string | null {
    return this.#entries.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.#entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.#entries.delete(key);
  }

  setItem(key: string, value: string): void {
    this.#entries.set(key, String(value));
  }
}

if (typeof globalThis.Storage === "undefined") {
  Object.defineProperty(globalThis, "Storage", { value: MemoryStorage, writable: true });
}

if (!globalThis.localStorage) {
  const storage = new MemoryStorage();
  Object.defineProperty(globalThis, "localStorage", { value: storage, writable: true });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", { value: storage, writable: true });
  }
}

// jsdom leaves scrollTo unimplemented and logs an error each time the router
// restores the scroll position after a navigation.
if (typeof window !== "undefined") {
  window.scrollTo = () => {};
}
