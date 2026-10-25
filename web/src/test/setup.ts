import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";

import { forgetSession } from "@/api/client";

import { server } from "./server";

// What jsdom lacks and the components use: media queries (the theme, the sidebar's mobile switch) at a desktop
// width, pointer capture and scrolling (Radix Select)
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
