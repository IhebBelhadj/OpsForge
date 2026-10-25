import "@testing-library/jest-dom/vitest";

import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";

import { forgetSession } from "@/api/client";

import { server } from "./server";

// A whole route's first render (lazy chunks, MSW) can take over a second on a loaded machine
configure({ asyncUtilTimeout: 3000 });

// What jsdom lacks and the components use: media queries (the theme, the sidebar's mobile switch) at a desktop
// width, pointer capture and scrolling (Radix Select), ResizeObserver (resizable panels)
window.matchMedia = (query: string) =>
  ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }) as unknown as MediaQueryList;
Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
Element.prototype.hasPointerCapture = () => false;
Element.prototype.releasePointerCapture = () => undefined;
Element.prototype.scrollIntoView = () => undefined;
window.ResizeObserver = class {
  observe() {
    return undefined;
  }
  unobserve() {
    return undefined;
  }
  disconnect() {
    return undefined;
  }
};

beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" });
});
afterEach(() => {
  cleanup();
  server.resetHandlers();
  forgetSession();
});
afterAll(() => {
  server.close();
});
