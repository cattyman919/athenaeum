import { describe, expect, it } from "vitest";
import { formatAvg, hasRating } from "./rating";

describe("rating display", () => {
  it("hides unrated books", () => {
    expect(hasRating({})).toBe(false);
    expect(hasRating({ rating_avg: null, rating_count: null })).toBe(false);
    expect(hasRating({ rating_avg: 4, rating_count: 0 })).toBe(false);
  });
  it("shows rated books with one decimal", () => {
    expect(hasRating({ rating_avg: 4.6, rating_count: 12 })).toBe(true);
    expect(formatAvg(4)).toBe("4.0");
    expect(formatAvg(4.6)).toBe("4.6");
  });
});
