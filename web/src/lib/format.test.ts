import { describe, expect, it } from "vitest";

import { count, duration } from "./format";

describe("format", () => {
  it("writes durations as people say them", () => {
    expect([8, 60, 370].map(duration)).toEqual(["8 min", "1 h", "6 h 10 min"]);
  });
  it("counts with the right plural", () => {
    expect([count(1, "lab"), count(3, "lab"), count(2, "quiz", "quizzes")]).toEqual(["1 lab", "3 labs", "2 quizzes"]);
  });
});
